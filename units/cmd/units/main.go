// Command units 提供单位/温度表达式解析与计算的 HTTP 服务。
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"units/internal/eval"
	"units/internal/parse"
)

type evalRequest struct {
	Expression string `json:"expression"`
	Target     string `json:"target"` // 空或 "auto" 表示自动
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Pos     int    `json:"pos"`
		End     int    `json:"end"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleEval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST"})
		return
	}
	var req evalRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"code": "BAD_REQUEST", "message": "请求体不是合法 JSON：" + err.Error()},
		})
		return
	}
	ast, err := parse.Parse(req.Expression)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := eval.Eval(ast, req.Target)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func handleEvalRange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only POST"})
		return
	}
	var req evalRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{
			"code": "BAD_REQUEST", "message": "请求体不是合法 JSON：" + err.Error(),
		}})
		return
	}
	ast, err := parse.Parse(req.Expression)
	if err != nil {
		writeErr(w, err)
		return
	}
	result, err := eval.EvalRange(ast, req.Target)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)}

func writeErr(w http.ResponseWriter, err error) {
	var pe parse.Error
	var ee eval.Error
	var ae apiError
	switch {
	case errors.As(err, &pe):
		ae.Error.Code = pe.Code
		ae.Error.Message = pe.Message
		ae.Error.Pos = pe.Pos
		ae.Error.End = pe.End
	case errors.As(err, &ee):
		ae.Error.Code = ee.Code
		ae.Error.Message = ee.Message
		ae.Error.Pos = ee.Pos
		ae.Error.End = ee.End
	default:
		ae.Error.Code = "INTERNAL"
		ae.Error.Message = err.Error()
	}
	writeJSON(w, http.StatusBadRequest, ae)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/api/eval", handleEval)
	mux.HandleFunc("/api/eval-range", handleEvalRange)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("units service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
