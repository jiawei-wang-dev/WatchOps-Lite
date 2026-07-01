package evidence

import "testing"

func TestAggregateDeduplicatesGroupsAndRanks(t *testing.T) {
	lowScore := 0.2
	highScore := 0.9
	aggregation := Aggregate([]Candidate{
		{
			Item: Item{
				ID:      "log-1",
				Type:    TypeLogEvent,
				Source:  SourceLogs,
				Content: "log",
			},
			LatencyMS: 20,
		},
		{
			Item: Item{
				ID:      "knowledge-low",
				Type:    TypeKnowledgeChunk,
				Source:  SourceKnowledge,
				Content: "low",
				Score:   &lowScore,
			},
			LatencyMS: 10,
		},
		{
			Item: Item{
				ID:      "knowledge-high",
				Type:    TypeKnowledgeChunk,
				Source:  SourceKnowledge,
				Content: "high",
				Score:   &highScore,
			},
			LatencyMS: 30,
		},
		{
			Item: Item{
				ID:      "log-1",
				Type:    TypeLogEvent,
				Source:  SourceLogs,
				Content: "duplicate with lower latency",
			},
			LatencyMS: 5,
		},
	})

	if len(aggregation.Items) != 3 {
		t.Fatalf("items = %#v, want three unique items", aggregation.Items)
	}
	if aggregation.Items[0].ID != "log-1" ||
		aggregation.Items[0].Content != "duplicate with lower latency" {
		t.Fatalf("first item = %#v, want lower-latency log duplicate", aggregation.Items[0])
	}
	if aggregation.Items[1].ID != "knowledge-high" ||
		aggregation.Items[2].ID != "knowledge-low" {
		t.Fatalf("knowledge order = %#v, want descending score", aggregation.Items[1:])
	}
	if aggregation.GroupCounts()["logs"] != 1 ||
		aggregation.GroupCounts()["knowledge"] != 2 {
		t.Fatalf("group counts = %#v", aggregation.GroupCounts())
	}
}
