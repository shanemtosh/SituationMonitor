package store

import (
	"context"
	"database/sql"
	"time"
)

// Quote is a cached market snapshot row.
type Quote struct {
	Symbol    string
	Name      string
	Price     float64
	ChangePct float64
	Currency  string
	FetchedAt time.Time
}

// UpsertQuote saves or replaces a quote row.
func UpsertQuote(ctx context.Context, db *sql.DB, q Quote) error {
	t := q.FetchedAt.UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
INSERT INTO market_quotes (symbol, name, price, change_pct, currency, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(symbol) DO UPDATE SET
	name = excluded.name,
	price = excluded.price,
	change_pct = excluded.change_pct,
	currency = excluded.currency,
	fetched_at = excluded.fetched_at
`, q.Symbol, q.Name, q.Price, q.ChangePct, q.Currency, t)
	return err
}

// ListQuotes returns recent quotes ordered by symbol.
func ListQuotes(ctx context.Context, db *sql.DB) ([]Quote, error) {
	rows, err := db.QueryContext(ctx, `
SELECT symbol, COALESCE(name,''), price, change_pct, COALESCE(currency,''), fetched_at
FROM market_quotes
ORDER BY symbol ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Quote
	for rows.Next() {
		var q Quote
		var fetched string
		if err := rows.Scan(&q.Symbol, &q.Name, &q.Price, &q.ChangePct, &q.Currency, &fetched); err != nil {
			return nil, err
		}
		q.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
		out = append(out, q)
	}
	return out, rows.Err()
}
