package diagnostics

import (
	"testing"
)

func BenchmarkRedactValue(b *testing.B) {
	doc := map[string]any{
		"theme": "dark",
		"folders": []any{
			map[string]any{
				"name": "a",
				"creds": map[string]any{
					"PASSWORD": "p1",
					"apiKey":   "k1",
					"other":    "keep",
				},
			},
			map[string]any{
				"name": "b",
				"creds": map[string]any{
					"password": "p2",
					"api_key":  "k2",
					"other":    "keep2",
				},
			},
		},
		"some_other_key": "some_value",
		"another_key":    "another_value",
		"deeply": map[string]any{
			"nested": map[string]any{
				"secret": "s1",
				"value":  "v1",
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		redactValue(doc, SecretKeys)
	}
}
