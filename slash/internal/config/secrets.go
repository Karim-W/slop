package config

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const keyringService = "slash-sftp"

func LoadPassword(siteID string) (string, bool) {
	password, err := keyring.Get(keyringService, siteID)
	if err != nil {
		return "", false
	}
	return password, true
}

func SavePassword(siteID, password string) error {
	return keyring.Set(keyringService, siteID, password)
}

func ForgetPassword(siteID string) error {
	err := keyring.Delete(keyringService, siteID)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
