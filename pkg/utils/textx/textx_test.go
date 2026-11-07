package textx

import "testing"

func TestShaping(t *testing.T) {
	cases := []struct{ got, want string }{
		{Truncate("hello world", 5), "hell…"},
		{Pad("hi", 4), "hi  "},
		{PadLeft("hi", 4), "  hi"},
		{Center("hi", 6), "  hi  "},
		{TruncateMiddle("/home/user/project/src", 12), "/ho…ject/src"},
		{Pluralize(1, "file", "files"), "1 file"},
		{Pluralize(3, "file", "files"), "3 files"},
		{Initials("Ada Lovelace"), "AL"},
		{Initials("ada"), "AD"},
		{Pad("\x1b[31mred\x1b[0m", 5), "\x1b[31mred\x1b[0m  "},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if Width("日本") != 4 {
		t.Error("wide runes take two cells")
	}
}
