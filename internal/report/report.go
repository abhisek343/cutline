package report

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"

	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/contracts"
	"github.com/abhisek343/cutline/internal/evidence"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/signature"
)

type Data struct {
	Status      string              `json:"status"`
	Signature   signature.Signature `json:"signature"`
	Schedule    explorer.Schedule   `json:"schedule"`
	View        evidence.View       `json:"view"`
	Evaluations []contracts.Result  `json:"evaluations"`
}

func Render(root, format string) ([]byte, error) {
	if _, err := capsule.ValidateDirectory(root); err != nil {
		return nil, err
	}
	data, err := capsule.ReadArtifact(root, "report/data.json")
	if err != nil {
		return nil, err
	}
	if format == "json" {
		return data, nil
	}
	if format != "" && format != "html" {
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	var value Data
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode report data: %w", err)
	}
	return htmlBytes(value), nil
}

func Write(root, destination, format string) error {
	data, err := Render(root, format)
	if err != nil {
		return err
	}
	if destination == "" {
		return fmt.Errorf("report destination is required")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("report destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func htmlBytes(data Data) []byte {
	status := html.EscapeString(data.Status)
	contract := html.EscapeString(data.Signature.Contract)
	digest := html.EscapeString(data.Signature.Digest)
	payload, _ := json.Marshal(data)
	return []byte("<!doctype html><meta charset=\"utf-8\"><title>Cutline report</title><style>body{font:16px system-ui;max-width:70rem;margin:2rem auto;padding:0 1rem}code,pre{white-space:pre-wrap} .status{font-weight:700}</style><main><h1>Cutline static report</h1><p>Status: <span class=\"status\">" + status + "</span></p><p>Contract: " + contract + "</p><p>Signature: <code>" + digest + "</code></p><h2>Evidence data</h2><pre>" + html.EscapeString(string(payload)) + "</pre></main>\n")
}
