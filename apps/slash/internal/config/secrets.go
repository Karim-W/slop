package config

import "github.com/karim-w/slop/pkg/infra/keychain"

// passwords lives under the same keychain service as before the monorepo
// move, so remembered passwords keep working.
var passwords = keychain.New("slash-sftp")

func LoadPassword(siteID string) (string, bool) { return passwords.Get(siteID) }

func SavePassword(siteID, password string) error { return passwords.Set(siteID, password) }

func ForgetPassword(siteID string) error { return passwords.Delete(siteID) }
