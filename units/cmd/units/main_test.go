package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postEval(t *testing.T, h http.Handler, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/eval", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHTTPSuccess(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/api/eval", handleEval)
	status, out := postEval(t, h, `{"expression":"100 C - 0 C","target":"dF"}`)
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, out)
	}
	root := out["root"].(map[string]any)
	if root["kind"] != "delta" {
		t.Fatalf("kind=%v", root["kind"])
	}
	val := root["value"].(map[string]any)
	if val["exact"] != "180" {
		t.Fatalf("100 dK in dF = %v, want 180", val["exact"])
	}
	steps := out["steps"].([]any)
	if len(steps) < 3 {
		t.Fatalf("steps=%d", len(steps))
	}
}

func TestHTTPBadExpression(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/api/eval", handleEval)
	status, out := postEval(t, h, `{"expression":"20 C * 2"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d", status)
	}
	errObj := out["error"].(map[string]any)
	if errObj["code"] != "ABSOLUTE_TEMPERATURE_MULTIPLY" {
		t.Fatalf("code=%v", errObj["code"])
	}
	if errObj["pos"].(float64) != 0 || errObj["end"].(float64) != 7 {
		t.Fatalf("span=%v:%v", errObj["pos"], errObj["end"])
	}
}

func TestHTTPBadJSON(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/api/eval", handleEval)
	status, out := postEval(t, h, `{not json`)
	if status != http.StatusBadRequest || !strings.Contains(out["error"].(map[string]any)["message"].(string), "JSON") {
		t.Fatalf("status=%d body=%v", status, out)
	}
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handleHealthz := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	handleHealthz.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("healthz=%d %s", rec.Code, rec.Body.String())
	}
}
