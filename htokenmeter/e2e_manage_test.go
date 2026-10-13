package main_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lxp/htokenmeter/model"
)

// F019: quota_usd=0 must mean "frozen", not be rewritten to unlimited (-1).
func TestTokenQuotaZeroFreezes(t *testing.T) {
	h := setup(t)

	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/token",
		strings.NewReader(`{"name":"frozen","quota_usd":0}`))
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	var out struct {
		Data model.Token `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Data.QuotaUsd != 0 {
		t.Fatalf("quota = %v, want 0 (frozen)", out.Data.QuotaUsd)
	}

	dbt, err := model.GetTokenByKey(out.Data.Key)
	if err != nil {
		t.Fatal(err)
	}
	if dbt.QuotaUsd != 0 {
		t.Fatalf("stored quota = %v, want 0", dbt.QuotaUsd)
	}
	if err := dbt.Validate(); err == nil {
		t.Fatal("frozen token (quota 0) passed validation")
	}
}

// F019: absent quota stays -1 (unlimited) on create and update.
func TestTokenQuotaAbsentMeansUnlimited(t *testing.T) {
	h := setup(t)

	full := h.createToken(t, "no-quota")
	dbt, err := model.GetTokenByKey(full)
	if err != nil {
		t.Fatal(err)
	}
	if dbt.QuotaUsd != -1 {
		t.Fatalf("absent quota = %v, want -1", dbt.QuotaUsd)
	}
}

// F019: update can set quota 0 and it survives the round trip.
func TestTokenUpdateQuotaZero(t *testing.T) {
	h := setup(t)

	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/api/token",
		strings.NewReader(`{"name":"upd"}`))
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data model.Token `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Data.Id == 0 {
		t.Fatal("create returned no id")
	}

	payload := `{"id":` + strconv.Itoa(out.Data.Id) + `,"name":"upd","status":1,"expired_time":-1,"quota_usd":0}`
	req, _ = http.NewRequest(http.MethodPut, h.srv.URL+"/api/token", strings.NewReader(payload))
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("update status %d", resp.StatusCode)
	}

	dbt, err := model.GetTokenByKey(out.Data.Key)
	if err != nil {
		t.Fatal(err)
	}
	if dbt.QuotaUsd != 0 {
		t.Fatalf("updated quota = %v, want 0", dbt.QuotaUsd)
	}
}

// F020: list responses must not carry full keys; create response must.
func TestTokenListMasksKey(t *testing.T) {
	h := setup(t)
	full := h.createToken(t, "masked")
	if len(full) < 16 {
		t.Fatalf("create returned suspiciously short key %q", full)
	}

	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/api/token", nil)
	req.Header.Set("Cookie", "htokenmeter_session="+h.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), full) {
		t.Fatal("list response leaks the full key")
	}
	if !strings.Contains(string(body), full[:8]+"...") {
		t.Fatalf("list response lacks masked key, got: %s", body)
	}
}
