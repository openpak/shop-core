package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Rewards is the one points ledger (platforms/core/infra/rewards). A console shop lists game cosmetics and
// sells them for points through it; shop-core only relays, so every console shares one path and one ledger.
// Points only: nothing here is ever a price in money.
type Rewards struct {
	Base   string // the ledger's internal URL
	Key    string // the events key
	Client *http.Client
}

func (c *Rewards) do(ctx context.Context, method, path string, q url.Values, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Internal-Key", c.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.Client
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b, err
}

// namespaceOf is the identity namespace a console's players are known by in the account core.
func namespaceOf(console string) string { return console }

func (s *Server) rewardsOrFail(w http.ResponseWriter) bool {
	if s.Rewards == nil || s.Rewards.Base == "" || s.Rewards.Key == "" {
		fail(w, http.StatusServiceUnavailable, "rewards_not_configured", "game items need SHOP_CORE_REWARDS_URL and SHOP_CORE_REWARDS_KEY")
		return false
	}
	return true
}

func relay(w http.ResponseWriter, code int, body []byte, err error) {
	if err != nil {
		fail(w, http.StatusBadGateway, "rewards_unavailable", "the Rewards ledger did not answer")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// gameItems: GET /v1/shop/{console}/game-items?subject=<console identity>[&game=<id>]
// Answers {game, account, balance, items:[{game, game_ref, slot, name, price, owned}]}: what a player can unlock for
// Rewards points, with the player's points. No game lists every game's items. An unlinked identity gets the list with 0 points.
func (s *Server) gameItems(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok || !s.rewardsOrFail(w) {
		return
	}
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		fail(w, http.StatusBadRequest, "subject_required", "subject (the player's console identity) is required")
		return
	}
	q := url.Values{"namespace": {namespaceOf(console)}, "subject": {subject}}
	if g := r.URL.Query().Get("game"); g != "" {
		q.Set("game", g)
	}
	code, b, err := s.Rewards.do(r.Context(), "GET", "/internal/v1/adapter/shop", q, nil)
	relay(w, code, b, err)
}

// redeemGameItem: POST /v1/shop/{console}/game-items/redeem {subject, game, game_ref, expected_price?, trans_id?}
// Unlocks one item for points through the ledger. Rewards answers: 200 {balance, game_ref, item}; 402 insufficient (with
// the balance); 409 price_mismatch | owned | not_for_sale; 404 the identity is not linked to an OpenPak account.
// A retried trans_id is charged once.
func (s *Server) redeemGameItem(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok || !s.rewardsOrFail(w) {
		return
	}
	var in struct {
		Subject       string `json:"subject"`
		Game          string `json:"game"`
		GameRef       string `json:"game_ref"`
		ExpectedPrice *int   `json:"expected_price"`
		TransID       string `json:"trans_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Subject == "" || in.Game == "" || in.GameRef == "" {
		fail(w, http.StatusBadRequest, "invalid_field", "subject, game and game_ref are required")
		return
	}
	body := map[string]any{"namespace": namespaceOf(console), "subject": in.Subject, "game": in.Game, "game_ref": in.GameRef, "trans_id": in.TransID}
	if in.ExpectedPrice != nil {
		body["expected_price"] = *in.ExpectedPrice
	}
	code, b, err := s.Rewards.do(r.Context(), "POST", "/internal/v1/adapter/redeem", nil, body)
	relay(w, code, b, err)
}
