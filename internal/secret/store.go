package secret

import (
	"errors"
	"sync"
)

// Store is a password backend. Production uses the OS keychain; tests use Memory.
type Store interface {
	Get(id string) (string, error)
	Set(id, password string) error
	Delete(id string) error
}

// Memory is an in-process Store. It never touches the OS keychain.
type Memory struct {
	mu   sync.Mutex
	data map[string]string
}

func NewMemory() *Memory {
	return &Memory{data: map[string]string{}}
}

func (m *Memory) Get(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pw, ok := m.data[id]
	if !ok {
		return "", ErrNotFound
	}
	return pw, nil
}

func (m *Memory) Set(id, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[id] = password
	return nil
}

func (m *Memory) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func mapKeyringErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, keyringNotFound) {
		return ErrNotFound
	}
	return err
}
