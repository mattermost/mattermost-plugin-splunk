package store

import (
	"bytes"
	"encoding/gob"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/pkg/errors"
)

// API that store uses for interactions with KVStore
type API interface {
	KVGet(key string) ([]byte, *model.AppError)
	KVSet(key string, value []byte) *model.AppError
	KVDelete(key string) *model.AppError
}

// KVStore abstraction for plugin.API.KVStore
type KVStore interface {
	Load(key string) ([]byte, error)
	Store(key string, data []byte) error
	Delete(key string) error
}

type store struct {
	api API
}

// NewStore creates KVStore from plugin.API
func NewStore(api API) KVStore {
	return &store{
		api: api,
	}
}

func (s *store) Load(key string) ([]byte, error) {
	data, appErr := s.api.KVGet(key)
	if appErr != nil {
		return nil, errors.WithMessage(appErr, "failed plugin KVGet")
	}
	if data == nil {
		return nil, errors.New("error while loading data from KVStore")
	}
	return data, nil
}

func (s *store) Store(key string, data []byte) error {
	appErr := s.api.KVSet(key, data)
	if appErr != nil {
		return errors.Wrapf(appErr, "Error while storing data with KVStore with key : %q", key)
	}
	return nil
}

func (s *store) Delete(key string) error {
	appErr := s.api.KVDelete(key)
	if appErr != nil {
		return errors.Wrapf(appErr, "Error while deleting data from KVStore with key : %q", key)
	}
	return nil
}

// LoadGOB load json data from KVStore
func LoadGOB(s KVStore, key string, v interface{}) (returnErr error) {
	data, err := s.Load(key)
	if err != nil {
		return errors.Wrap(err, "Error while loading json")
	}
	return gob.NewDecoder(bytes.NewBuffer(data)).Decode(v)
}

// SetGOB sets json data in KVStore
func SetGOB(s KVStore, key string, v interface{}) (returnErr error) {
	data := bytes.Buffer{}
	err := gob.NewEncoder(&data).Encode(v)
	if err != nil {
		return errors.Wrap(err, "Error while storing json")
	}
	return s.Store(key, data.Bytes())
}
