package store

const autoScheduleFlowInfinity = int(^uint(0) >> 2)

type autoScheduleFlowEdge struct {
	to       int
	reverse  int
	capacity int
}

type autoScheduleMaxFlowGraph struct {
	graph [][]autoScheduleFlowEdge
	level []int
	next  []int
}

func autoScheduleMaxFlow(slots []*autoScheduleSlot, memberCount, perSlot, capacityUnits int) int {
	return autoScheduleRemainingMaxFlow(slots, memberCount, perSlot, capacityUnits, make([]int, memberCount))
}

func autoScheduleRemainingMaxFlow(slots []*autoScheduleSlot, memberCount, perSlot, capacityUnits int, forcedLoad []int) int {
	source := 0
	slotNodeStart := 1
	memberNodeStart := slotNodeStart + len(slots)*2
	sink := memberNodeStart + memberCount
	flow := newAutoScheduleMaxFlowGraph(sink + 1)

	for slotIndex, slot := range slots {
		for parity := 0; parity < 2; parity++ {
			oddWeek := parity == 0
			remainingDemand := autoScheduleDeficit(perSlot, len(slot.EvenTaken))
			if oddWeek {
				remainingDemand = autoScheduleDeficit(perSlot, len(slot.OddTaken))
			}
			if remainingDemand <= 0 {
				continue
			}
			node := slotNodeStart + slotIndex*2 + parity
			flow.addEdge(source, node, remainingDemand)
			candidates := slot.EvenCandidates
			taken := slot.EvenTaken
			if oddWeek {
				candidates = slot.OddCandidates
				taken = slot.OddTaken
			}
			for _, memberIndex := range candidates {
				if _, exists := taken[memberIndex]; exists {
					continue
				}
				flow.addEdge(node, memberNodeStart+memberIndex, 1)
			}
		}
	}

	for memberIndex := 0; memberIndex < memberCount; memberIndex++ {
		remainingCapacity := capacityUnits - forcedLoad[memberIndex]
		if remainingCapacity > 0 {
			flow.addEdge(memberNodeStart+memberIndex, sink, remainingCapacity)
		}
	}
	return flow.maxFlow(source, sink)
}

func newAutoScheduleMaxFlowGraph(nodeCount int) *autoScheduleMaxFlowGraph {
	return &autoScheduleMaxFlowGraph{
		graph: make([][]autoScheduleFlowEdge, nodeCount),
		level: make([]int, nodeCount),
		next:  make([]int, nodeCount),
	}
}

func (f *autoScheduleMaxFlowGraph) addEdge(from, to, capacity int) {
	forward := autoScheduleFlowEdge{to: to, reverse: len(f.graph[to]), capacity: capacity}
	reverse := autoScheduleFlowEdge{to: from, reverse: len(f.graph[from]), capacity: 0}
	f.graph[from] = append(f.graph[from], forward)
	f.graph[to] = append(f.graph[to], reverse)
}

func (f *autoScheduleMaxFlowGraph) maxFlow(source, sink int) int {
	total := 0
	for f.buildLevels(source, sink) {
		clear(f.next)
		for {
			pushed := f.push(source, sink, autoScheduleFlowInfinity)
			if pushed == 0 {
				break
			}
			total += pushed
		}
	}
	return total
}

func (f *autoScheduleMaxFlowGraph) buildLevels(source, sink int) bool {
	for index := range f.level {
		f.level[index] = -1
	}
	queue := make([]int, 1, len(f.graph))
	queue[0] = source
	f.level[source] = 0
	for head := 0; head < len(queue); head++ {
		from := queue[head]
		for _, edge := range f.graph[from] {
			if edge.capacity <= 0 || f.level[edge.to] >= 0 {
				continue
			}
			f.level[edge.to] = f.level[from] + 1
			queue = append(queue, edge.to)
		}
	}
	return f.level[sink] >= 0
}

func (f *autoScheduleMaxFlowGraph) push(from, sink, limit int) int {
	if from == sink {
		return limit
	}
	for f.next[from] < len(f.graph[from]) {
		edgeIndex := f.next[from]
		edge := &f.graph[from][edgeIndex]
		if edge.capacity > 0 && f.level[edge.to] == f.level[from]+1 {
			pushed := f.push(edge.to, sink, min(limit, edge.capacity))
			if pushed > 0 {
				edge.capacity -= pushed
				f.graph[edge.to][edge.reverse].capacity += pushed
				return pushed
			}
		}
		f.next[from]++
	}
	return 0
}

type autoScheduleCostEdge struct {
	to       int
	reverse  int
	capacity int
	cost     int
}

type autoScheduleCostFlow struct {
	graph  [][]autoScheduleCostEdge
	source int
	sink   int
}

func newAutoScheduleCostFlow(nodeCount, source, sink int) *autoScheduleCostFlow {
	return &autoScheduleCostFlow{
		graph:  make([][]autoScheduleCostEdge, nodeCount),
		source: source,
		sink:   sink,
	}
}

func (f *autoScheduleCostFlow) addEdge(from, to, capacity, cost int) int {
	edgeIndex := len(f.graph[from])
	forward := autoScheduleCostEdge{to: to, reverse: len(f.graph[to]), capacity: capacity, cost: cost}
	reverse := autoScheduleCostEdge{to: from, reverse: edgeIndex, capacity: 0, cost: -cost}
	f.graph[from] = append(f.graph[from], forward)
	f.graph[to] = append(f.graph[to], reverse)
	return edgeIndex
}

func (f *autoScheduleCostFlow) minCostMaxFlow(target int) (int, int) {
	flow := 0
	totalCost := 0
	nodeCount := len(f.graph)
	for flow < target {
		distance := make([]int, nodeCount)
		previousNode := make([]int, nodeCount)
		previousEdge := make([]int, nodeCount)
		inQueue := make([]bool, nodeCount)
		for index := range distance {
			distance[index] = autoScheduleFlowInfinity
			previousNode[index] = -1
		}

		distance[f.source] = 0
		queue := []int{f.source}
		inQueue[f.source] = true
		for head := 0; head < len(queue); head++ {
			from := queue[head]
			inQueue[from] = false
			for edgeIndex, edge := range f.graph[from] {
				if edge.capacity <= 0 || distance[from] == autoScheduleFlowInfinity {
					continue
				}
				nextDistance := distance[from] + edge.cost
				if nextDistance >= distance[edge.to] {
					continue
				}
				distance[edge.to] = nextDistance
				previousNode[edge.to] = from
				previousEdge[edge.to] = edgeIndex
				if !inQueue[edge.to] {
					queue = append(queue, edge.to)
					inQueue[edge.to] = true
				}
			}
		}

		if previousNode[f.sink] < 0 {
			break
		}
		pushed := target - flow
		for node := f.sink; node != f.source; node = previousNode[node] {
			edge := f.graph[previousNode[node]][previousEdge[node]]
			pushed = min(pushed, edge.capacity)
		}
		for node := f.sink; node != f.source; node = previousNode[node] {
			from := previousNode[node]
			edgeIndex := previousEdge[node]
			edge := &f.graph[from][edgeIndex]
			edge.capacity -= pushed
			f.graph[edge.to][edge.reverse].capacity += pushed
		}
		flow += pushed
		totalCost += pushed * distance[f.sink]
	}
	return flow, totalCost
}
