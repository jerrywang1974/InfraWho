package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jerrywang1974/InfraWho/internal/config"
	"github.com/jerrywang1974/InfraWho/internal/db"
	"github.com/jerrywang1974/InfraWho/internal/vault"
)

const rewrapBatchSize = 100

func runKeys(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: infrawho keys rewrap --from <ver> --to <ver> --old-key-file <path> --new-key-file <path>")
	}
	switch args[0] {
	case "rewrap":
		return runKeysRewrap(args[1:])
	case "help", "-h", "--help":
		printKeysUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown keys subcommand %q; try: infrawho keys rewrap", args[0])
	}
}

func printKeysUsage(w io.Writer) {
	fmt.Fprint(w, `infrawho keys — KEK maintenance commands

Commands:
  rewrap   Classic DEK rewrap under a new KEK (stop HTTP first; see runbook below)

Rewrap usage:
  infrawho keys rewrap --from kek-v1 --to kek-v2 \
    --old-key-file /etc/infrawho/master-v1.key \
    --new-key-file /etc/infrawho/master-v2.key

Uses INFRAWHO_DB_URL for the SQLite database (same as the server).

Stop-the-world runbook:
  1. Stop the HTTP service (e.g. docker compose stop infrawho).
  2. Run rewrap until it reports 0 remaining rows on --from.
     If interrupted: keep both key files, re-run rewrap, do not start the app yet.
  3. Point INFRAWHO_MASTER_KEY_FILE only at the new key file.
  4. Start the service and confirm GET /readyz succeeds.

Rewrap only unwraps/wraps DEKs and updates key_version. It does not change
nonce or ciphertext, and does not decrypt business plaintext.
`)
}

func runKeysRewrap(args []string) error {
	fs := flag.NewFlagSet("keys rewrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fromVer := fs.String("from", "", "current key_version to migrate from (required)")
	toVer := fs.String("to", "", "new key_version to migrate to (required)")
	oldKeyFile := fs.String("old-key-file", "", "path to the from-KEK file (required)")
	newKeyFile := fs.String("new-key-file", "", "path to the to-KEK file (required)")
	fs.Usage = func() { printKeysUsage(os.Stderr) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	*fromVer = strings.TrimSpace(*fromVer)
	*toVer = strings.TrimSpace(*toVer)
	*oldKeyFile = strings.TrimSpace(*oldKeyFile)
	*newKeyFile = strings.TrimSpace(*newKeyFile)

	if *fromVer == "" || *toVer == "" || *oldKeyFile == "" || *newKeyFile == "" {
		printKeysUsage(os.Stderr)
		return fmt.Errorf("--from, --to, --old-key-file, and --new-key-file are required")
	}
	if *fromVer == *toVer {
		return fmt.Errorf("--from and --to must differ")
	}

	fromKEK, err := config.LoadMasterKey(*oldKeyFile)
	if err != nil {
		return fmt.Errorf("old key: %w", err)
	}
	toKEK, err := config.LoadMasterKey(*newKeyFile)
	if err != nil {
		return fmt.Errorf("new key: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(cfg.DBURL)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer sqlDB.Close()

	updated, err := rewrapSecretPayloads(sqlDB, *fromVer, *toVer, fromKEK, toKEK)
	if err != nil {
		return err
	}

	var remaining int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM secret_payloads WHERE key_version = ?`, *fromVer,
	).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining: %w", err)
	}

	fmt.Printf("rewrap complete: updated=%d remaining_from=%d from=%s to=%s\n",
		updated, remaining, *fromVer, *toVer)
	if remaining != 0 {
		return fmt.Errorf("%d rows still on %s; fix errors and re-run (keep both key files)", remaining, *fromVer)
	}
	return nil
}

func rewrapSecretPayloads(sqlDB *sql.DB, fromVer, toVer string, fromKEK, toKEK []byte) (int, error) {
	total := 0
	for {
		n, err := rewrapBatch(sqlDB, fromVer, toVer, fromKEK, toKEK, rewrapBatchSize)
		if err != nil {
			return total, err
		}
		total += n
		if n == 0 {
			return total, nil
		}
	}
}

func rewrapBatch(sqlDB *sql.DB, fromVer, toVer string, fromKEK, toKEK []byte, limit int) (int, error) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(
		`SELECT account_id, wrapped_dek FROM secret_payloads WHERE key_version = ? LIMIT ?`,
		fromVer, limit,
	)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	type item struct {
		accountID  string
		wrappedDEK []byte
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.accountID, &it.wrappedDEK); err != nil {
			return 0, fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, it)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("rows: %w", err)
	}
	_ = rows.Close()

	if len(batch) == 0 {
		return 0, nil
	}

	stmt, err := tx.Prepare(
		`UPDATE secret_payloads
		 SET wrapped_dek = ?, key_version = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE account_id = ? AND key_version = ?`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	updated := 0
	for _, it := range batch {
		newWrapped, err := vault.RewrapDEK(it.wrappedDEK, fromKEK, toKEK)
		if err != nil {
			return updated, fmt.Errorf("rewrap account_id=%s: %w", it.accountID, err)
		}
		res, err := stmt.Exec(newWrapped, toVer, it.accountID, fromVer)
		if err != nil {
			return updated, fmt.Errorf("update account_id=%s: %w", it.accountID, err)
		}
		n, _ := res.RowsAffected()
		updated += int(n)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return updated, nil
}
