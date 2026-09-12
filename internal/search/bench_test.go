package search_test

import (
	"fmt"
	"testing"

	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/search"
)

// Aspirational FTS bench: seed N assets and time MATCH.
func BenchmarkSearch(b *testing.B) {
	sqlDB, err := db.Open("sqlite::memory:")
	if err != nil {
		b.Fatal(err)
	}
	defer sqlDB.Close()

	const n = 500
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("a%04d", i)
		host := fmt.Sprintf("host-%04d.example", i)
		purpose := "web"
		if i%10 == 0 {
			purpose = "postgres primary"
		}
		if _, err := sqlDB.Exec(`
			INSERT INTO assets (id, name, hostname, asset_type, os_family, environment, purpose, status)
			VALUES (?, ?, ?, 'vm', 'linux', 'lab', ?, 'active')`,
			id, "asset-"+host, host, purpose); err != nil {
			b.Fatal(err)
		}
	}

	store := search.NewStore(sqlDB)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, total, err := store.SearchAssets(search.Filter{Q: "postgres", Limit: 50})
		if err != nil {
			b.Fatal(err)
		}
		if total == 0 || len(hits) == 0 {
			b.Fatalf("expected hits, total=%d", total)
		}
	}
}
