package library

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

// Stream describes the audio inside a file, read from its headers without
// decoding: enough to show "FLAC 24/96 5.1" and a duration in the library.
// Formats kyro can't probe here get their details from the player once
// they play.
type Stream struct {
	Codec      string // FLAC, ALAC, AAC, MP3, WAV, AIFF, Opus, Vorbis, …
	SampleRate int
	BitDepth   int // 0 for lossy codecs
	Channels   int
	Duration   time.Duration
}

var errUnknown = errors.New("unknown format")

func probe(path string) (Stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return Stream{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Stream{}, err
	}
	head := make([]byte, 12)
	if _, err := io.ReadFull(f, head); err != nil {
		return Stream{}, errUnknown
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Stream{}, err
	}
	switch {
	case bytes.HasPrefix(head, []byte("fLaC")):
		return probeFLAC(f)
	case bytes.Equal(head[4:8], []byte("ftyp")):
		return probeMP4(f, fi.Size())
	case bytes.HasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		return probeWAV(f)
	case bytes.HasPrefix(head, []byte("FORM")) && (bytes.Equal(head[8:12], []byte("AIFF")) || bytes.Equal(head[8:12], []byte("AIFC"))):
		return probeAIFF(f)
	case bytes.HasPrefix(head, []byte("ID3")) || head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return probeMP3(f, fi.Size())
	case bytes.HasPrefix(head, []byte("OggS")):
		return probeOgg(f, fi.Size())
	}
	return Stream{}, errUnknown
}

// ─── FLAC ───────────────────────────────────────────────────────────────────

func probeFLAC(r io.Reader) (Stream, error) {
	b := make([]byte, 4+4+34) // magic, block header, STREAMINFO
	if _, err := io.ReadFull(r, b); err != nil {
		return Stream{}, err
	}
	si := b[8:]
	rate := int(si[10])<<12 | int(si[11])<<4 | int(si[12])>>4
	ch := int(si[12]>>1&0x07) + 1
	bps := int(si[12]&0x01)<<4 | int(si[13]>>4) + 1
	total := uint64(si[13]&0x0F)<<32 | uint64(binary.BigEndian.Uint32(si[14:18]))
	s := Stream{Codec: "FLAC", SampleRate: rate, BitDepth: bps, Channels: ch}
	if rate > 0 {
		s.Duration = time.Duration(float64(total) / float64(rate) * float64(time.Second))
	}
	return s, nil
}

// ─── MP4 / M4A (ALAC, AAC, AC-3, E-AC-3) ────────────────────────────────────

func probeMP4(r io.ReadSeeker, size int64) (Stream, error) {
	var s Stream
	var walk func(end int64, path string) error
	walk = func(end int64, path string) error {
		for {
			pos, _ := r.Seek(0, io.SeekCurrent)
			if pos+8 > end {
				return nil
			}
			var hdr [8]byte
			if _, err := io.ReadFull(r, hdr[:]); err != nil {
				return nil
			}
			n := int64(binary.BigEndian.Uint32(hdr[:4]))
			typ := string(hdr[4:])
			hl := int64(8)
			if n == 1 {
				var ext [8]byte
				if _, err := io.ReadFull(r, ext[:]); err != nil {
					return nil
				}
				n, hl = int64(binary.BigEndian.Uint64(ext[:])), 16
			} else if n == 0 {
				n = end - pos
			}
			if n < hl {
				return errUnknown
			}
			body := pos + hl
			next := pos + n
			switch typ {
			case "moov", "trak", "mdia", "minf", "stbl":
				if err := walk(next, path+"/"+typ); err != nil {
					return err
				}
			case "mvhd":
				b := make([]byte, min(n-hl, 32))
				io.ReadFull(r, b)
				if len(b) >= 20 && b[0] == 1 && len(b) >= 32 {
					scale := binary.BigEndian.Uint32(b[20:24])
					dur := binary.BigEndian.Uint64(b[24:32])
					if scale > 0 {
						s.Duration = time.Duration(float64(dur) / float64(scale) * float64(time.Second))
					}
				} else if len(b) >= 20 {
					scale := binary.BigEndian.Uint32(b[12:16])
					dur := binary.BigEndian.Uint32(b[16:20])
					if scale > 0 {
						s.Duration = time.Duration(float64(dur) / float64(scale) * float64(time.Second))
					}
				}
			case "stsd":
				if s.Codec == "" {
					readStsd(r, n-hl, &s)
				}
			}
			if _, err := r.Seek(next, io.SeekStart); err != nil {
				return nil
			}
			_ = body
		}
	}
	if err := walk(size, ""); err != nil {
		return Stream{}, err
	}
	if s.Codec == "" {
		return s, errUnknown
	}
	return s, nil
}

// readStsd reads the first sample entry of an audio track.
func readStsd(r io.Reader, n int64, s *Stream) {
	b := make([]byte, min(n, 256))
	if _, err := io.ReadFull(r, b); err != nil || len(b) < 8+36 {
		return
	}
	entry := b[8:] // skip version/flags and entry count
	format := string(entry[4:8])
	// AudioSampleEntry: 8 header, 6 reserved, 2 index, 8 reserved,
	// 2 channels, 2 sample size, 4 reserved, 4 rate (16.16)
	ch := int(binary.BigEndian.Uint16(entry[24:26]))
	bits := int(binary.BigEndian.Uint16(entry[26:28]))
	rate := int(binary.BigEndian.Uint32(entry[32:36]) >> 16)
	switch format {
	case "alac":
		s.Codec = "ALAC"
		// The ALAC magic cookie ('alac' box inside the entry) is exact,
		// including rates above 65535 Hz that the 16.16 field can't hold.
		if i := bytes.Index(entry[36:], []byte("alac")); i >= 0 {
			c := entry[36+i+4:]
			if len(c) >= 4+24 {
				c = c[4:] // version/flags
				bits = int(c[5])
				ch = int(c[9])
				rate = int(binary.BigEndian.Uint32(c[20:24]))
			}
		}
	case "mp4a":
		s.Codec, bits = "AAC", 0
	case "ac-3":
		s.Codec, bits = "AC-3", 0
	case "ec-3":
		s.Codec, bits = "E-AC-3", 0
	case "Opus":
		s.Codec, bits = "Opus", 0
	case "fLaC":
		s.Codec = "FLAC"
	default:
		return
	}
	s.Channels, s.BitDepth, s.SampleRate = ch, bits, rate
}

// ─── WAV / AIFF ─────────────────────────────────────────────────────────────

func probeWAV(r io.ReadSeeker) (Stream, error) {
	s := Stream{Codec: "WAV"}
	r.Seek(12, io.SeekStart)
	var byteRate uint32
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			break
		}
		n := int64(binary.LittleEndian.Uint32(hdr[4:]))
		switch string(hdr[:4]) {
		case "fmt ":
			b := make([]byte, min(n, 40))
			io.ReadFull(r, b)
			if len(b) >= 16 {
				s.Channels = int(binary.LittleEndian.Uint16(b[2:4]))
				s.SampleRate = int(binary.LittleEndian.Uint32(b[4:8]))
				byteRate = binary.LittleEndian.Uint32(b[8:12])
				s.BitDepth = int(binary.LittleEndian.Uint16(b[14:16]))
			}
			r.Seek(n-int64(len(b))+n%2, io.SeekCurrent)
		case "data":
			if byteRate > 0 {
				s.Duration = time.Duration(float64(n) / float64(byteRate) * float64(time.Second))
			}
			return s, nil
		default:
			r.Seek(n+n%2, io.SeekCurrent)
		}
	}
	if s.SampleRate == 0 {
		return s, errUnknown
	}
	return s, nil
}

func probeAIFF(r io.ReadSeeker) (Stream, error) {
	s := Stream{Codec: "AIFF"}
	r.Seek(12, io.SeekStart)
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			break
		}
		n := int64(binary.BigEndian.Uint32(hdr[4:]))
		if string(hdr[:4]) == "COMM" {
			b := make([]byte, 18)
			if _, err := io.ReadFull(r, b); err != nil {
				break
			}
			s.Channels = int(binary.BigEndian.Uint16(b[0:2]))
			frames := binary.BigEndian.Uint32(b[2:6])
			s.BitDepth = int(binary.BigEndian.Uint16(b[6:8]))
			s.SampleRate = int(extended80(b[8:18]))
			if s.SampleRate > 0 {
				s.Duration = time.Duration(float64(frames) / float64(s.SampleRate) * float64(time.Second))
			}
			return s, nil
		}
		r.Seek(n+n%2, io.SeekCurrent)
	}
	return s, errUnknown
}

// extended80 decodes AIFF's 80-bit IEEE 754 extended float.
func extended80(b []byte) float64 {
	exp := int(binary.BigEndian.Uint16(b[0:2])&0x7FFF) - 16383
	mant := binary.BigEndian.Uint64(b[2:10])
	f := float64(mant) / float64(uint64(1)<<63)
	for ; exp > 0; exp-- {
		f *= 2
	}
	for ; exp < 0; exp++ {
		f /= 2
	}
	return f
}

// ─── MP3 ────────────────────────────────────────────────────────────────────

var mp3Rates = [4][3]int{{11025, 12000, 8000}, {}, {22050, 24000, 16000}, {44100, 48000, 32000}}

var mp3Bitrates = map[bool][16]int{ // MPEG-1 layer III, MPEG-2/2.5 layer III (kbit/s)
	true:  {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	false: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
}

func probeMP3(r io.ReadSeeker, size int64) (Stream, error) {
	start := int64(0)
	var id3 [10]byte
	if _, err := io.ReadFull(r, id3[:]); err == nil && string(id3[:3]) == "ID3" {
		start = 10 + int64(id3[6])<<21 | int64(id3[7])<<14 | int64(id3[8])<<7 | int64(id3[9])
	}
	r.Seek(start, io.SeekStart)
	buf := make([]byte, 64*1024)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		h := binary.BigEndian.Uint32(buf[i:])
		version := (h >> 19) & 3 // 3 = MPEG-1, 2 = MPEG-2, 0 = MPEG-2.5
		layer := (h >> 17) & 3
		bi := (h >> 12) & 0xF
		ri := (h >> 10) & 3
		if version == 1 || layer != 1 || bi == 0 || bi == 15 || ri == 3 {
			continue // not a layer III frame header
		}
		rate := mp3Rates[version][ri]
		mono := (h>>6)&3 == 3
		s := Stream{Codec: "MP3", SampleRate: rate, Channels: 2}
		if mono {
			s.Channels = 1
		}
		samplesPerFrame := 1152
		if version != 3 {
			samplesPerFrame = 576
		}
		// Xing/Info header: exact frame count for VBR files.
		side := 32
		switch {
		case version == 3 && mono:
			side = 17
		case version != 3 && !mono:
			side = 17
		case version != 3 && mono:
			side = 9
		}
		x := i + 4 + side
		if x+12 <= len(buf) && (string(buf[x:x+4]) == "Xing" || string(buf[x:x+4]) == "Info") {
			if flags := binary.BigEndian.Uint32(buf[x+4:]); flags&1 != 0 {
				frames := binary.BigEndian.Uint32(buf[x+8:])
				s.Duration = time.Duration(float64(frames) * float64(samplesPerFrame) / float64(rate) * float64(time.Second))
				return s, nil
			}
		}
		kbps := mp3Bitrates[version == 3][bi]
		if kbps > 0 {
			s.Duration = time.Duration(float64(size-start-int64(i)) * 8 / float64(kbps*1000) * float64(time.Second))
		}
		return s, nil
	}
	return Stream{}, errUnknown
}

// ─── Ogg (Opus, Vorbis, FLAC) ───────────────────────────────────────────────

func probeOgg(r io.ReadSeeker, size int64) (Stream, error) {
	head := make([]byte, 4096)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	var s Stream
	switch {
	case bytes.Contains(head, []byte("OpusHead")):
		i := bytes.Index(head, []byte("OpusHead"))
		if i+19 > len(head) {
			return s, errUnknown
		}
		s = Stream{Codec: "Opus", Channels: int(head[i+9]), SampleRate: 48000}
		if in := int(binary.LittleEndian.Uint32(head[i+12:])); in > 0 {
			s.SampleRate = in // the source rate; Opus always decodes at 48 kHz
		}
	case bytes.Contains(head, []byte("\x01vorbis")):
		i := bytes.Index(head, []byte("\x01vorbis"))
		if i+16 > len(head) {
			return s, errUnknown
		}
		s = Stream{Codec: "Vorbis", Channels: int(head[i+11]), SampleRate: int(binary.LittleEndian.Uint32(head[i+12:]))}
	default:
		return s, errUnknown
	}
	// Duration: granule position of the last page.
	tail := int64(64 * 1024)
	if size < tail {
		tail = size
	}
	r.Seek(size-tail, io.SeekStart)
	end := make([]byte, tail)
	m, _ := io.ReadFull(r, end)
	end = end[:m]
	if i := bytes.LastIndex(end, []byte("OggS")); i >= 0 && i+14 <= len(end) {
		gran := binary.LittleEndian.Uint64(end[i+6:])
		rate := s.SampleRate
		if s.Codec == "Opus" {
			rate = 48000 // granule positions are always 48 kHz for Opus
		}
		if rate > 0 {
			s.Duration = time.Duration(float64(gran) / float64(rate) * float64(time.Second))
		}
	}
	return s, nil
}
