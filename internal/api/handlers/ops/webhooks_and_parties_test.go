package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

func TestOpsV0104WebhookNoLoopbackLimitAndWatchPartyFixes(t *testing.T) {
	h, _, _, _, bus := setupOpsTest(t)
	authMW := middleware.NewAuth(nil, nil)
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	host := makeAuthCtx("host-v104", "host", false)
	member := makeAuthCtx("member-v104", "member", false)
	outsider := makeAuthCtx("outsider-v104", "outsider", false)
	admin := makeAuthCtx("admin-v104", "admin", true)

	// 1. Default webhook HTTP client blocks loopback (127.0.0.1) directly at DialContext level
	loopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer loopSrv.Close()
	if _, err := h.sendWebhook(context.Background(), WebhookConfig{Url: loopSrv.URL}, WebhookPayload{Event: "Test"}); err == nil {
		t.Fatalf("expected default SafeHTTPClientNoLoopback to block loopback webhook dial")
	}

	// 2. sendWebhook caps response body draining via LimitReader(4096) and succeeds cleanly on huge response
	var bytesWritten atomic.Int64
	hugeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		n, _ := w.Write(bytes.Repeat([]byte("X"), 64*1024))
		bytesWritten.Add(int64(n))
	}))
	defer hugeSrv.Close()
	h.SetHTTPClient(netutil.NewSafeHTTPClient(10*time.Second, true))
	code, err := h.sendWebhook(context.Background(), WebhookConfig{Url: hugeSrv.URL}, WebhookPayload{Event: "Test"})
	if err != nil || code != http.StatusOK {
		t.Fatalf("sendWebhook huge response failed: code=%d err=%v", code, err)
	}

	// 3. Watch Party: Public room visible in GET /Parties for all logged-in users; InviteCode stripped for non-host/non-admin & webhook
	var webhookBody atomic.Value
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		webhookBody.Store(string(raw))
		w.WriteHeader(http.StatusOK)
	}))
	defer hookSrv.Close()

	if r := doAuthReq(t, authMux, admin, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     hookSrv.URL,
		"Events":  []string{"SessionUpdated"},
		"Enabled": true,
	}); r.Code != http.StatusOK {
		t.Fatalf("create webhook: %d %s", r.Code, r.Body.String())
	}

	// Public room (explicit IsPublic=true, no invite code)
	pubRec := doAuthReq(t, authMux, host, http.MethodPost, "/Parties", map[string]any{
		"Name":     "Public Cinema",
		"IsPublic": true,
	})
	if pubRec.Code != http.StatusOK {
		t.Fatalf("create public party: %d", pubRec.Code)
	}
	var pubParty Party
	_ = json.Unmarshal(pubRec.Body.Bytes(), &pubParty)

	// Private room with InviteCode
	privRec := doAuthReq(t, authMux, host, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Private Lounge",
		"IsPublic":   false,
		"InviteCode": "TOP-SECRET-INVITE",
	})
	if privRec.Code != http.StatusOK {
		t.Fatalf("create private party: %d", privRec.Code)
	}
	var privParty Party
	_ = json.Unmarshal(privRec.Body.Bytes(), &privParty)
	if privParty.InviteCode != "TOP-SECRET-INVITE" {
		t.Fatalf("host should see own InviteCode on create, got %q", privParty.InviteCode)
	}

	// Outsider GET /Parties MUST see public room, MUST NOT see private room
	listOut := doAuthReq(t, authMux, outsider, http.MethodGet, "/Parties", nil)
	if listOut.Code != http.StatusOK {
		t.Fatalf("outsider GET /Parties status=%d", listOut.Code)
	}
	var outParties []Party
	_ = json.Unmarshal(listOut.Body.Bytes(), &outParties)
	if len(outParties) != 1 || outParties[0].Id != pubParty.Id {
		t.Fatalf("expected outsider to see only public party %s, got %+v", pubParty.Id, outParties)
	}

	// Member joins private room with invite code -> InviteCode hidden in Join, Get, List, and State responses!
	joinRec := doAuthReq(t, authMux, member, http.MethodPost, "/Parties/"+privParty.Id+"/Join", map[string]any{"InviteCode": "TOP-SECRET-INVITE"})
	if joinRec.Code != http.StatusOK {
		t.Fatalf("member join private party: %d", joinRec.Code)
	}
	var joined Party
	_ = json.Unmarshal(joinRec.Body.Bytes(), &joined)
	if joined.InviteCode != "" {
		t.Fatalf("expected InviteCode hidden for non-host member on Join, got %q", joined.InviteCode)
	}

	getMem := doAuthReq(t, authMux, member, http.MethodGet, "/Parties/"+privParty.Id, nil)
	if strings.Contains(getMem.Body.String(), "TOP-SECRET-INVITE") {
		t.Fatalf("expected InviteCode hidden for non-host member on GET /Parties/{Id}: %s", getMem.Body.String())
	}
	listMem := doAuthReq(t, authMux, member, http.MethodGet, "/Parties", nil)
	if strings.Contains(listMem.Body.String(), "TOP-SECRET-INVITE") {
		t.Fatalf("expected InviteCode hidden for non-host member on GET /Parties: %s", listMem.Body.String())
	}
	// Host and Admin DO see InviteCode
	getHost := doAuthReq(t, authMux, host, http.MethodGet, "/Parties/"+privParty.Id, nil)
	if !strings.Contains(getHost.Body.String(), "TOP-SECRET-INVITE") {
		t.Fatalf("expected host to see InviteCode: %s", getHost.Body.String())
	}
	getAdmin := doAuthReq(t, authMux, admin, http.MethodGet, "/Parties/"+privParty.Id, nil)
	if !strings.Contains(getAdmin.Body.String(), "TOP-SECRET-INVITE") {
		t.Fatalf("expected admin to see InviteCode: %s", getAdmin.Body.String())
	}

	// Update party state -> triggers EventBus & Webhook; webhook payload MUST strip InviteCode!
	pos := int64(42000)
	stateRec := doAuthReq(t, authMux, member, http.MethodPost, "/Parties/"+privParty.Id+"/State", map[string]any{"PositionTicks": pos})
	if stateRec.Code != http.StatusOK {
		t.Fatalf("member state update: %d", stateRec.Code)
	}
	if strings.Contains(stateRec.Body.String(), "TOP-SECRET-INVITE") {
		t.Fatalf("expected InviteCode hidden in State response for member: %s", stateRec.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := webhookBody.Load().(string); s != "" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	gotHook, _ := webhookBody.Load().(string)
	if gotHook == "" {
		t.Fatalf("expected webhook delivery for party state update")
	}
	if strings.Contains(gotHook, "TOP-SECRET-INVITE") {
		t.Fatalf("webhook payload leaked InviteCode: %s", gotHook)
	}

	// Also verify sanitizeWebhookPayloadData handles *Party directly
	sanPtr := sanitizeWebhookPayloadData(&Party{Id: "p", InviteCode: "LEAK"}).(*Party)
	if sanPtr.InviteCode != "" {
		t.Fatalf("expected *Party InviteCode stripped")
	}
	_ = bus

	// 4. Outsider cannot call Leave on a party they are not a member of (403)
	if r := doAuthReq(t, authMux, outsider, http.MethodPost, "/Parties/"+privParty.Id+"/Leave", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member Leave, got %d body=%s", r.Code, r.Body.String())
	}

	// 5. Per-user max active parties quota (10): host already owns 2, create 8 more -> 11th returns 429, while member can still create
	for i := 0; i < maxPartiesPerUser-2; i++ {
		r := doAuthReq(t, authMux, host, http.MethodPost, "/Parties", map[string]any{"Name": "Room"})
		if r.Code != http.StatusOK {
			t.Fatalf("expected host party #%d to succeed, got %d", i+3, r.Code)
		}
	}
	if r := doAuthReq(t, authMux, host, http.MethodPost, "/Parties", map[string]any{"Name": "OverQuota"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 11th party for same user, got %d body=%s", r.Code, r.Body.String())
	}
	if r := doAuthReq(t, authMux, member, http.MethodPost, "/Parties", map[string]any{"Name": "MemberRoom"}); r.Code != http.StatusOK {
		t.Fatalf("expected different user to create party despite host quota, got %d", r.Code)
	}
}
