package threads

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/govdbot/govd/internal/models"
)

func TestDumpExtracted(t *testing.T) {
	body, err := os.ReadFile("/tmp/t_post2_body.html")
	if err != nil {
		t.Skip("no body")
	}
	ctx := &models.ExtractorContext{}
	m, err := ParsePostMedia(ctx, body)
	if err != nil {
		t.Fatalf("ParsePostMedia error: %v", err)
	}
	fmt.Printf("caption=%q\n", m.Caption)
	fmt.Printf("items=%d\n", len(m.Items))
	for i, it := range m.Items {
		b, _ := json.MarshalIndent(it, "  ", "  ")
		fmt.Printf("--- item %d ---\n%s\n", i, string(b))
	}
}
