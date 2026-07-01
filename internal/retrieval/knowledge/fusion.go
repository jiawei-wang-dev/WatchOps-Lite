package knowledge

import "sort"

func fuseRRF(
	bm25Results []SearchResult,
	vectorResults []SearchResult,
	rrfK int,
	limit int,
) []SearchResult {
	type fusedResult struct {
		result SearchResult
		score  float64
	}
	byChunk := make(map[string]*fusedResult, len(bm25Results)+len(vectorResults))
	add := func(results []SearchResult, mode string) {
		for index, result := range results {
			entry, ok := byChunk[result.ChunkID]
			if !ok {
				copy := result
				copy.RetrievalMode = "hybrid"
				entry = &fusedResult{result: copy}
				byChunk[result.ChunkID] = entry
			}
			entry.score += 1 / float64(rrfK+index+1)
			score := result.Score
			if mode == "bm25" {
				entry.result.BM25Score = &score
			} else {
				entry.result.VectorScore = &score
			}
		}
	}
	add(bm25Results, "bm25")
	add(vectorResults, "vector")

	fused := make([]SearchResult, 0, len(byChunk))
	for _, entry := range byChunk {
		score := entry.score
		entry.result.Score = score
		entry.result.RRFScore = &score
		entry.result.Metadata = cloneMetadata(entry.result.Metadata)
		fused = append(fused, entry.result)
	}
	sort.SliceStable(fused, func(left int, right int) bool {
		if fused[left].Score == fused[right].Score {
			return fused[left].ChunkID < fused[right].ChunkID
		}
		return fused[left].Score > fused[right].Score
	})
	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused
}
