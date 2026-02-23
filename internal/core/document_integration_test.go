package core_test

import (
	"context"
	"sync"
	"testing"

	"accounting-agent/internal/core"
)

func TestDocumentService_ConcurrentPosting(t *testing.T) {
	pool := setupTestDB(t) // Skips if TEST_DATABASE_URL is not set
	defer pool.Close()

	// Ensure documents table has the valid type for tests since setupTestDB clears it
	_, err := pool.Exec(context.Background(), `
		INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy) 
		VALUES ('JE', 'Journal Entry', false, true, false, false, 'global', false)
		ON CONFLICT DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("failed to seed document type: %v", err)
	}

	docService := core.NewDocumentService(pool)
	ctx := context.Background()

	// 1. Create 10 draft documents
	var docIDs []int
	for i := 0; i < 10; i++ {
		// Using 'JE' type which is seeded globally by migration 005
		id, err := docService.CreateDraftDocument(ctx, 1, "JE", nil, nil)
		if err != nil {
			t.Fatalf("failed to create draft document: %v", err)
		}
		docIDs = append(docIDs, id)
	}

	// 2. Post all documents concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, len(docIDs))

	for _, id := range docIDs {
		wg.Add(1)
		go func(docID int) {
			defer wg.Done()
			if err := docService.PostDocument(ctx, docID); err != nil {
				errCh <- err
			}
		}(id)
	}

	wg.Wait()
	close(errCh)

	// Catch any errors from the goroutines
	for err := range errCh {
		t.Errorf("concurrent post error: %v", err)
	}

	// 3. Verify exactly 10 POSTED documents and exactly 10 unique document_numbers
	var count int
	err = pool.QueryRow(ctx, "SELECT count(DISTINCT document_number) FROM documents WHERE company_id = 1 AND type_code = 'JE' AND status = 'POSTED'").Scan(&count)
	if err != nil {
		t.Fatalf("failed to count unique document numbers: %v", err)
	}

	if count != 10 {
		t.Errorf("expected 10 unique document numbers, got %d", count)
	}
}
