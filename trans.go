package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	transMap   = map[string]string{}
	transFile  = ""
	transDirty = false
	transMu    sync.Mutex
)

func loadTransMap(path string) {
	transMu.Lock()
	defer transMu.Unlock()

	transFile = path
	data, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(data)) == "" {
		transMap = map[string]string{}
		transDirty = true
		saveTransMapLocked()
		return
	}
	if err := json.Unmarshal(data, &transMap); err != nil {
		fmt.Println("[WARN] trans.json 解析失败，不会覆盖原文件:", err)
		transMap = map[string]string{}
	}
}

func saveTransMapLocked() {
	if transFile == "" {
		return
	}
	data, err := json.MarshalIndent(transMap, "", "  ")
	if err != nil {
		return
	}
	tmpFile := transFile + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return
	}
	_ = f.Sync()
	_ = f.Close()
	if err := os.Rename(tmpFile, transFile); err != nil {
		return
	}
	transDirty = false
}

func startTransAutoSave() {
	go func() {
		ticker := time.NewTicker(autoSaveInterval)
		defer ticker.Stop()
		for range ticker.C {
			transMu.Lock()
			if transDirty {
				saveTransMapLocked()
			}
			transMu.Unlock()
		}
	}()
}

func addUntranslatedToTrans(text string) {
	text = normalizeText(text)
	if text == "" || !shouldSaveUntranslated(text) {
		return
	}
	plain := stripRichText(text)

	transMu.Lock()
	defer transMu.Unlock()

	if _, ok := transMap[text]; ok {
		return
	}
	if _, ok := transMap[plain]; ok {
		return
	}
	transMap[text] = ""
	transDirty = true
}

func localTranslate(text string) string {
	text = normalizeText(text)
	if text == "" {
		return ""
	}
	plain := stripRichText(text)

	transMu.Lock()
	defer transMu.Unlock()

	if v, ok := transMap[text]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := transMap[plain]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}

	out := text
	changed := false
	for jp, zh := range transMap {
		jp = normalizeText(jp)
		zh = strings.TrimSpace(zh)
		if jp == "" || zh == "" {
			continue
		}
		if strings.Contains(out, jp) {
			out = strings.ReplaceAll(out, jp, zh)
			changed = true
		}
	}
	if changed {
		return normalizeText(stripRichText(out))
	}
	return ""
}
