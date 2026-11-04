// Package nximport reads the Switch catalog from nx-shop's store admin API (GET only) and hands
// it to the store. shop-core never writes to nx-shop: until the Switch migrates (P7) nx-shop
// stays the source of truth and this is a one-way, read-only mirror.
package nximport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openpak/shop-core/internal/store"
)

type Client struct {
	Base string // e.g. http://openpak-nx-shop:20070
	Key  string
	HTTP *http.Client
}

type title struct {
	TitleID      string `json:"title_id"`
	Published    bool   `json:"published"`
	Publisher    string `json:"publisher"`
	Category     string `json:"category"`
	Version      int64  `json:"version"`
	VersionLabel string `json:"version_label"`
	RevisedAt    int64  `json:"revised_at"`
	UpdatedAt    int64  `json:"updated_at"`
	SizeBytes    int64  `json:"size_bytes"`
	Changelog    string `json:"changelog"`
	Distribution struct {
		Source     string `json:"source"`
		Permission string `json:"permission"`
	} `json:"distribution"`
	Localized map[string]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"localized"`
	DefaultLang string `json:"default_lang"`
}

// Fetch lists the Switch catalog.
func (c *Client) Fetch(ctx context.Context) ([]store.Imported, error) {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/internal/store/titles", nil)
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.Header.Set("X-Internal-Key", c.Key)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("nx-shop store api: status %d", resp.StatusCode)
	}
	var doc struct {
		Titles []json.RawMessage `json:"titles"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make([]store.Imported, 0, len(doc.Titles))
	for _, raw := range doc.Titles {
		var t title
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		if t.TitleID == "" {
			continue
		}
		out = append(out, convert(t, raw))
	}
	return out, nil
}

func convert(t title, raw json.RawMessage) store.Imported {
	name, desc := t.TitleID, ""
	pick := func(lang string) bool {
		if v, ok := t.Localized[lang]; ok && v.Name != "" {
			name, desc = v.Name, v.Description
			return true
		}
		return false
	}
	if !pick("en-US") && !pick(t.DefaultLang) {
		for lang := range t.Localized {
			if pick(lang) {
				break
			}
		}
	}
	kind := "nx-shop"
	im := store.Imported{Title: store.Title{
		Console: "switch", TitleID: t.TitleID, Name: name, Publisher: t.Publisher, Category: t.Category,
		Licence: t.Distribution.Permission, SourceKind: kind, SourceRef: t.Distribution.Source, Description: desc,
		Published: t.Published, Meta: raw,
	}}
	v := store.Version{Version: t.Version, VersionLabel: t.VersionLabel, Size: t.SizeBytes, Changelog: t.Changelog}
	ts := t.RevisedAt
	if ts == 0 {
		ts = t.UpdatedAt
	}
	if ts > 0 {
		at := time.Unix(ts, 0).UTC()
		v.RevisedAt = &at
	}
	im.Version = &v
	return im
}
