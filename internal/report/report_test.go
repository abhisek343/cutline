package report

import (
	"strings"
	"testing"

	"github.com/abhisek343/cutline/internal/signature"
)

func TestHTMLReportEscapesTargetContent(t *testing.T) {
	data := Data{Status: "violation<script>", Signature: signature.Signature{Contract: "<contract>", Digest: "sha256:test"}}
	html := string(htmlBytes(data))
	if strings.Contains(html, "<script>") || strings.Contains(html, "<contract>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("report was not escaped: %s", html)
	}
}
