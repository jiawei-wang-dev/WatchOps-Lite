package evidence

import "sort"

type Candidate struct {
	Item      Item
	LatencyMS int64
}

type Aggregation struct {
	Items  []Item
	Groups map[Source][]Item
}

type rankedCandidate struct {
	Candidate
	sequence int
}

func Aggregate(candidates []Candidate) Aggregation {
	unique := make(map[string]rankedCandidate, len(candidates))
	sourceOrder := make([]Source, 0, 4)
	seenSources := make(map[Source]struct{}, 4)
	nextAnonymous := 0
	for sequence, candidate := range candidates {
		candidate.Item = Normalize(candidate.Item, candidate.Item.Source)
		key := candidate.Item.ID
		if key == "" {
			nextAnonymous++
			key = anonymousKey(nextAnonymous)
		}
		ranked := rankedCandidate{Candidate: candidate, sequence: sequence}
		existing, exists := unique[key]
		if !exists || betterCandidate(ranked, existing) {
			unique[key] = ranked
		}
		if _, exists := seenSources[candidate.Item.Source]; !exists {
			seenSources[candidate.Item.Source] = struct{}{}
			sourceOrder = append(sourceOrder, candidate.Item.Source)
		}
	}

	rankedGroups := make(map[Source][]rankedCandidate, len(sourceOrder))
	for _, candidate := range unique {
		rankedGroups[candidate.Item.Source] = append(
			rankedGroups[candidate.Item.Source],
			candidate,
		)
	}
	result := Aggregation{
		Items:  make([]Item, 0, len(unique)),
		Groups: make(map[Source][]Item, len(rankedGroups)),
	}
	for _, source := range sourceOrder {
		group := rankedGroups[source]
		sort.SliceStable(group, func(left, right int) bool {
			return betterCandidate(group[left], group[right])
		})
		items := make([]Item, 0, len(group))
		for _, candidate := range group {
			items = append(items, candidate.Item)
			result.Items = append(result.Items, candidate.Item)
		}
		result.Groups[source] = items
	}
	return result
}

func (a Aggregation) GroupCounts() map[string]int {
	result := make(map[string]int, len(a.Groups))
	for source, items := range a.Groups {
		result[string(source)] = len(items)
	}
	return result
}

func betterCandidate(left, right rankedCandidate) bool {
	leftHasScore := left.Item.Score != nil
	rightHasScore := right.Item.Score != nil
	if leftHasScore != rightHasScore {
		return leftHasScore
	}
	if leftHasScore && *left.Item.Score != *right.Item.Score {
		return *left.Item.Score > *right.Item.Score
	}
	if left.LatencyMS != right.LatencyMS {
		return left.LatencyMS < right.LatencyMS
	}
	return left.sequence < right.sequence
}

func anonymousKey(index int) string {
	const digits = "0123456789"
	if index == 0 {
		return "#0"
	}
	buffer := make([]byte, 0, 8)
	for index > 0 {
		buffer = append(buffer, digits[index%10])
		index /= 10
	}
	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}
	return "#" + string(buffer)
}
