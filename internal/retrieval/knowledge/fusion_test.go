package knowledge

import "testing"

func TestFuseRRFMergesAndRanksSharedChunks(t *testing.T) {
	bm25 := []SearchResult{
		{ChunkID: "bm25-only", Score: 9},
		{ChunkID: "shared", Score: 8},
	}
	vector := []SearchResult{
		{ChunkID: "shared", Score: 0.95},
		{ChunkID: "vector-only", Score: 0.9},
	}

	results := fuseRRF(bm25, vector, 60, 3)
	if len(results) != 3 || results[0].ChunkID != "shared" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].BM25Score == nil ||
		results[0].VectorScore == nil ||
		results[0].RRFScore == nil ||
		results[0].RetrievalMode != "hybrid" {
		t.Fatalf("shared result = %#v", results[0])
	}
}
