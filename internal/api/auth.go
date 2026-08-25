package api

import (
	"crypto/rand"
	"encoding/hex"
)

// RandToken 生成随机 hex token，供 webhook 和临时用途使用
func RandToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
