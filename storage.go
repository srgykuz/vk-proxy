package main

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

const (
	storageNamespaceUnknown int = iota + 1
	storageNamespaceA
	storageNamespaceB
)

var storageMu = sync.Mutex{}
var storageNamespace = storageNamespaceUnknown
var storageNamespaceChangedAt = time.Time{}
var storageNextKey = 0

func listenStorage(cfg config, club configClub) error {
	params := storageGetParams{
		keys:   createStorageGetKeys(),
		userID: club.ID,
	}
	last, err := storageGet(cfg.API, club, params)

	if err != nil {
		return fmt.Errorf("club %v: %v", club.Name, err)
	}

	slog.Info("storage: listening", "club", club.Name)

	for {
		if !isSessionOpened() {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		current, err := storageGet(cfg.API, club, params)

		if err != nil {
			slog.Error("storage: listen", "club", club.Name, "err", err)
			time.Sleep(5 * time.Second)
			continue
		}

		changed := diffStorageValues(last, current)
		last = current

		for _, resp := range changed {
			go func(value string) {
				if err := handleStorageUpdate(cfg, club, value); err != nil {
					slog.Error("storage: update", "club", club.Name, "err", err)
				}
			}(resp.Value)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func handleStorageUpdate(cfg config, club configClub, value string) error {
	if len(value) == 0 {
		return nil
	}

	decideStorageNamespace(value)

	upd := update{
		Type: "storage_change",
		Object: updateObject{
			Text: value,
		},
	}

	return handleUpdate(cfg, club, upd)
}

func diffStorageValues(oldValues, newValues []storageGetResponse) []storageGetResponse {
	if len(oldValues) == 0 {
		return newValues
	}

	changed := []storageGetResponse{}
	oldMap := map[string]string{}

	for _, v := range oldValues {
		oldMap[v.Key] = v.Value
	}

	for _, v := range newValues {
		if oldValue, exists := oldMap[v.Key]; !exists || oldValue != v.Value {
			changed = append(changed, v)
		}
	}

	return changed
}

func decideStorageNamespace(value string) {
	storageMu.Lock()
	defer storageMu.Unlock()

	if time.Since(storageNamespaceChangedAt) < 10*time.Second {
		return
	}

	dg, err := decodeDatagram(value)

	if err != nil || dg.isLoopback() {
		return
	}

	me := deviceID
	interlocutor := dg.device
	newNamespace := storageNamespaceUnknown

	if me < interlocutor {
		newNamespace = storageNamespaceA
	} else if me > interlocutor {
		newNamespace = storageNamespaceB
	}

	if newNamespace != storageNamespace {
		slog.Debug("storage: namespace change", "old", storageNamespace, "new", newNamespace)
	}

	storageNamespace = newNamespace
	storageNamespaceChangedAt = time.Now()
}

func createStorageGetKeys() []string {
	keys := []string{}

	for i := 1; i <= 200; i++ {
		keys = append(keys, fmt.Sprintf("key-%v", i))
	}

	return keys
}

func createStorageSetKey() string {
	storageMu.Lock()
	defer storageMu.Unlock()

	key := 0

	if storageNamespace == storageNamespaceUnknown {
		key = rand.Intn(200) + 1
	} else {
		if storageNamespace == storageNamespaceA && (storageNextKey < 1 || storageNextKey > 100) {
			storageNextKey = 1
		} else if storageNamespace == storageNamespaceB && (storageNextKey < 101 || storageNextKey > 200) {
			storageNextKey = 101
		}

		key = storageNextKey
		storageNextKey++
	}

	return fmt.Sprintf("key-%v", key)
}
