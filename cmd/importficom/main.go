// importficom loads a FICOM m_employee export (pipe-delimited CSV, per
// ficom.m_employee's DDL) into the participants table, so the sales
// hierarchy (SD -> NSM -> GRSM -> RSM -> SS) becomes resolvable by the
// engine's "superior" and "role" resolver rules.
//
// m_employee already carries the full superior_id chain, so it maps
// directly onto participants — no need to go through mv_salesman_hierarchy,
// which is a denormalized view of a single branch.
//
// Defaults to --dry-run: prints a summary (counts per position, inactive
// count, rows skipped as test data) without writing anything. Pass
// -dry-run=false once the summary looks right.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"approval-engine-service/internal/config"
	"approval-engine-service/internal/database"
	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/repository"
)

// empTypePosition maps ficom.m_employee.emp_type_id to the position label
// stored on participants, following the level names mv_salesman_hierarchy
// itself uses (sd/nsm/grsm/rsm/ss).
var empTypePosition = map[string]string{
	"1": "SS",   // Sales Supervisor
	"2": "RSM",  // Regional Sales Manager
	"3": "GRSM", // Group Regional Sales Manager
	"4": "NSM",  // National Sales Manager
	"5": "SD",   // Sales Director (chain root)
	"6": "Director",
	"7": "BOD",
}

func main() {
	csvPath := flag.String("csv", "", "path to the m_employee CSV export (pipe-delimited)")
	dryRun := flag.Bool("dry-run", true, "print a summary only; pass -dry-run=false to actually upsert")
	department := flag.String("department", "Sales", "department value to stamp on every imported participant")
	batchSize := flag.Int("batch-size", 300, "participants per UpsertBatch transaction (smaller = more resilient to a dropped connection, at the cost of more round trips)")
	flag.Parse()

	if *csvPath == "" {
		log.Fatal("missing -csv <path to m_employee export>")
	}

	participants, skipped, err := loadCSV(*csvPath, *department)
	if err != nil {
		log.Fatalf("load csv: %v", err)
	}

	cleared := clearDanglingSuperiors(participants)
	participants, err = topoSortBySuperior(participants)
	if err != nil {
		log.Fatalf("sort by superior chain: %v", err)
	}

	printSummary(participants, skipped, cleared)

	if *dryRun {
		fmt.Println("\ndry run: nothing written. Re-run with -dry-run=false to upsert into participants.")
		return
	}

	cfg := config.Load()
	if !cfg.HasDatabase() {
		log.Fatal("DATABASE_URL is not set (check .env)")
	}
	db, err := database.NewTurso(cfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	repo := repository.NewParticipantRepository(db)
	ctx := context.Background()
	written := 0
	// Chunked rather than one giant transaction: each Upsert is idempotent
	// (ON CONFLICT DO UPDATE), so a dropped connection only costs the
	// in-flight chunk — just re-run the whole command, already-written
	// chunks upsert to the same values harmlessly. This only works because
	// participants is topologically sorted first (topoSortBySuperior): Turso
	// enforces the superior_id foreign key, and defer_foreign_keys only
	// defers the check to the end of *this* transaction — a superior must
	// already be committed in an earlier chunk, not merely present
	// somewhere later in the slice.
	for start := 0; start < len(participants); start += *batchSize {
		end := start + *batchSize
		if end > len(participants) {
			end = len(participants)
		}
		chunk := participants[start:end]
		if err := repo.UpsertBatch(ctx, chunk); err != nil {
			log.Fatalf("upsert batch [%d:%d] (%d of %d written so far): %v", start, end, written, len(participants), err)
		}
		written += len(chunk)
		fmt.Printf("  upserted %d/%d\n", written, len(participants))
	}
	fmt.Printf("\nupserted %d participants\n", written)
}

type skippedRow struct {
	empID  string
	name   string
	reason string
}

// loadCSV parses the export and applies the m_employee -> participants
// mapping:
//   - user_id      = emp_id
//   - name         = full_name if present, else emp_nm (full_name is empty
//     on almost every row in this export; emp_nm carries the
//     territory/position label instead of a person's name —
//     that's how FICOM models this hierarchy)
//   - position     = empTypePosition[emp_type_id]
//   - department   = the -department flag value, for every row
//   - superior_id  = superior_id, empty -> nil (root of the chain)
//   - is_active    = is_terminate != "Y"
//
// Rows whose name starts with "test" (case-insensitive) are skipped: FICOM
// export itself contains at least one such row (emp_type_id 7, "Test BOD"),
// and importing it would let the resolver hand approvals to a placeholder.
func loadCSV(path, department string) ([]domain.Participant, []skippedRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = '|'
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, want := range []string{"emp_id", "emp_nm", "emp_type_id", "superior_id", "email", "is_terminate", "full_name"} {
		if _, ok := col[want]; !ok {
			return nil, nil, fmt.Errorf("csv missing expected column %q", want)
		}
	}
	get := func(rec []string, name string) string {
		i := col[name]
		if i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var participants []domain.Participant
	var skipped []skippedRow
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read row: %w", err)
		}

		empID := get(rec, "emp_id")
		if empID == "" {
			continue
		}
		name := get(rec, "full_name")
		if name == "" {
			name = get(rec, "emp_nm")
		}
		if strings.HasPrefix(strings.ToLower(name), "test") {
			skipped = append(skipped, skippedRow{empID: empID, name: name, reason: "name looks like test data"})
			continue
		}

		empType := get(rec, "emp_type_id")
		position, ok := empTypePosition[empType]
		if !ok {
			skipped = append(skipped, skippedRow{empID: empID, name: name, reason: fmt.Sprintf("unknown emp_type_id %q", empType)})
			continue
		}

		p := domain.Participant{
			UserID:     empID,
			Name:       name,
			Email:      get(rec, "email"),
			Position:   position,
			Department: department,
			IsActive:   get(rec, "is_terminate") != "Y",
		}
		if sup := get(rec, "superior_id"); sup != "" {
			p.SuperiorID = &sup
		}
		participants = append(participants, p)
	}

	return participants, skipped, nil
}

// clearDanglingSuperiors nils out SuperiorID (in place) for any participant
// whose superior isn't in this import — Turso enforces the superior_id
// foreign key, and the DB starts empty, so such a reference would fail at
// commit time no matter how the batch is chunked. Returns the ones changed,
// for the summary: each one becomes a root of its own chain instead of
// dead-ending mid-resolve.
func clearDanglingSuperiors(participants []domain.Participant) []domain.Participant {
	ids := make(map[string]bool, len(participants))
	for _, p := range participants {
		ids[p.UserID] = true
	}
	var cleared []domain.Participant
	for i, p := range participants {
		if p.SuperiorID != nil && !ids[*p.SuperiorID] {
			cleared = append(cleared, p)
			participants[i].SuperiorID = nil
		}
	}
	return cleared
}

// topoSortBySuperior orders participants so every superior appears strictly
// before the participants reporting to them — required so fixed-size
// chunked transactions (see main) never split a superior from a subordinate
// across a chunk boundary the wrong way. Standard Kahn's-algorithm
// topological sort over the superior_id edges.
func topoSortBySuperior(participants []domain.Participant) ([]domain.Participant, error) {
	byID := make(map[string]domain.Participant, len(participants))
	children := make(map[string][]string) // superior_id -> direct reports
	indegree := make(map[string]int, len(participants))
	for _, p := range participants {
		byID[p.UserID] = p
		indegree[p.UserID] = 0
	}
	for _, p := range participants {
		if p.SuperiorID != nil {
			children[*p.SuperiorID] = append(children[*p.SuperiorID], p.UserID)
			indegree[p.UserID]++
		}
	}

	var queue []string
	for _, p := range participants {
		if indegree[p.UserID] == 0 {
			queue = append(queue, p.UserID)
		}
	}
	sort.Strings(queue) // deterministic output for a given input

	sorted := make([]domain.Participant, 0, len(participants))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byID[id])

		next := append([]string(nil), children[id]...)
		sort.Strings(next)
		for _, childID := range next {
			indegree[childID]--
			if indegree[childID] == 0 {
				queue = append(queue, childID)
			}
		}
	}

	if len(sorted) != len(participants) {
		return nil, fmt.Errorf("superior_id cycle detected: sorted %d of %d participants", len(sorted), len(participants))
	}
	return sorted, nil
}

func printSummary(participants []domain.Participant, skipped []skippedRow, cleared []domain.Participant) {
	byPosition := map[string]int{}
	inactive := 0
	for _, p := range participants {
		byPosition[p.Position]++
		if !p.IsActive {
			inactive++
		}
	}

	fmt.Printf("parsed %d participants to import (%d skipped)\n", len(participants), len(skipped))
	positions := make([]string, 0, len(byPosition))
	for pos := range byPosition {
		positions = append(positions, pos)
	}
	sort.Strings(positions)
	for _, pos := range positions {
		fmt.Printf("  %-8s %d\n", pos, byPosition[pos])
	}
	fmt.Printf("  inactive (is_terminate=Y): %d\n", inactive)

	if len(cleared) > 0 {
		fmt.Printf("\n%d participants referenced a superior_id not present anywhere in this import — cleared to make them chain roots:\n", len(cleared))
		for _, p := range cleared {
			fmt.Printf("  %s %q\n", p.UserID, p.Name)
		}
	}

	if len(skipped) > 0 {
		fmt.Println("\nskipped rows:")
		for _, s := range skipped {
			fmt.Printf("  %s %q: %s\n", s.empID, s.name, s.reason)
		}
	}
}
