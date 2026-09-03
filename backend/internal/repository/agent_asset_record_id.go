package repository

import (
	"crypto/sha256"
	"encoding/hex"
)

func agentAssetRecordID(namespace string, sourceID string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + sourceID))
	value := hex.EncodeToString(digest[:16])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}
