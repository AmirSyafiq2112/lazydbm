package secret

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "lazydbm"

func Get(id string) (string, error) {
	pw, err := keyring.Get(service, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return pw, err
}

func Set(id, password string) error {
	return keyring.Set(service, id, password)
}

func Delete(id string) error {
	err := keyring.Delete(service, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

var ErrNotFound = errors.New("password not found")
