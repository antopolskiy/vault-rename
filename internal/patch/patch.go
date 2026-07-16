package patch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/antopolskiy/vault-rename/internal/model"
)

func Apply(data []byte, patches []model.Patch) ([]byte, error) {
	ordered := append([]model.Patch(nil), patches...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Start < ordered[j].Start
	})

	var out bytes.Buffer
	cursor := 0
	for _, item := range ordered {
		if item.Start < cursor || item.Start < 0 || item.End < item.Start || item.End > len(data) {
			return nil, fmt.Errorf("invalid or overlapping patch at %d:%d", item.Start, item.End)
		}
		if !bytes.Equal(data[item.Start:item.End], item.Before) {
			return nil, fmt.Errorf("patch source bytes changed at %d:%d", item.Start, item.End)
		}
		out.Write(data[cursor:item.Start])
		out.Write(item.After)
		cursor = item.End
	}
	out.Write(data[cursor:])
	return out.Bytes(), nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
