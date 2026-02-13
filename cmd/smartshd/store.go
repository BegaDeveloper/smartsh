package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var jobsBucket = []byte("jobs")
var approvalsBucket = []byte("approvals")

type jobStore struct {
	db *bolt.DB
}

func dbPathFromEnv() string {
	if path := os.Getenv("SMARTSH_DAEMON_DB"); path != "" {
		return path
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".smartshd.db"
	}
	return filepath.Join(homeDir, ".smartsh", "smartshd.db")
}

func newJobStore(path string) (*jobStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory failed: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, createErr := tx.CreateBucketIfNotExists(jobsBucket)
		if createErr != nil {
			return createErr
		}
		_, createApprovalErr := tx.CreateBucketIfNotExists(approvalsBucket)
		return createApprovalErr
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &jobStore{db: db}, nil
}

func (store *jobStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *jobStore) Save(job daemonJob) error {
	return store.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(jobsBucket)
		payload, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(job.ID), payload)
	})
}

func (store *jobStore) Get(jobID string) (*daemonJob, error) {
	var job *daemonJob
	err := store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(jobsBucket)
		raw := bucket.Get([]byte(jobID))
		if raw == nil {
			return nil
		}
		parsed := daemonJob{}
		if decodeErr := json.Unmarshal(raw, &parsed); decodeErr != nil {
			return decodeErr
		}
		job = &parsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (store *jobStore) List(limit int) ([]daemonJob, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	result := make([]daemonJob, 0, limit)
	err := store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(jobsBucket)
		cursor := bucket.Cursor()
		for key, value := cursor.Last(); key != nil && len(result) < limit; key, value = cursor.Prev() {
			job := daemonJob{}
			if decodeErr := json.Unmarshal(value, &job); decodeErr != nil {
				continue
			}
			result = append(result, job)
		}
		return nil
	})
	return result, err
}

func (store *jobStore) SaveApproval(approval commandApproval) error {
	return store.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(approvalsBucket)
		payload, err := json.Marshal(approval)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(approval.ID), payload)
	})
}

func (store *jobStore) GetApproval(approvalID string) (*commandApproval, error) {
	var approval *commandApproval
	err := store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(approvalsBucket)
		raw := bucket.Get([]byte(approvalID))
		if raw == nil {
			return nil
		}
		parsed := commandApproval{}
		if decodeErr := json.Unmarshal(raw, &parsed); decodeErr != nil {
			return decodeErr
		}
		approval = &parsed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return approval, nil
}
