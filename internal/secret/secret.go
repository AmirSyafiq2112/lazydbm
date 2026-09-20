package secret

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const service = "lazydbm"

var (
	ErrNotFound           = errors.New("password not found")
	keyringNotFound       = keyring.ErrNotFound
	Default         Store = keyringStore{}
)

func Get(id string) (string, error) { return Default.Get(id) }
func Set(id, password string) error { return Default.Set(id, password) }
func Delete(id string) error        { return Default.Delete(id) }

type keyringStore struct{}

func (keyringStore) Get(id string) (string, error) {
	pw, err := keyring.Get(service, id)
	if err != nil {
		return "", mapKeyringErr(err)
	}
	return pw, nil
}

func (keyringStore) Set(id, password string) error {
	return keyring.Set(service, id, password)
}

func (keyringStore) Delete(id string) error {
	return mapKeyringDeleteErr(keyring.Delete(service, id))
}

func mapKeyringDeleteErr(err error) error {
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
