// Lattice Placement Evidence builds the same deterministic corpus with a
// topic-clustered policy and an intentionally interleaved policy. It uses only
// the public HexxlaDB API and emits aggregate placement-quality evidence.
//
// Usage:
//
//	go run ./examples/lattice_placement_evidence
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/hexxla/hexxladb"
)

const (
	vectorDimension      = 12
	maxDocumentsPerTopic = 100
	maxPlacementRadius   = 128
)

type config struct {
	documentsPerTopic  int
	initialPerTopic    int
	neighborhoodRadius int
	maxCells           int
	semanticK          int
	seed               uint64
}

type topic struct {
	id      string
	label   string
	summary string
	anchor  hexxladb.Coord
}

var topics = []topic{
	{id: "storage", label: "S", summary: "storage pages, transactions, and recovery", anchor: hexxladb.Coord{Q: -24, R: 0}},
	{id: "search", label: "Q", summary: "semantic and lexical retrieval quality", anchor: hexxladb.Coord{Q: 0, R: -24}},
	{id: "security", label: "C", summary: "keys, trust boundaries, and tamper handling", anchor: hexxladb.Coord{Q: 24, R: -24}},
	{id: "operations", label: "O", summary: "backup, compaction, and incident response", anchor: hexxladb.Coord{Q: 24, R: 0}},
	{id: "testing", label: "T", summary: "deterministic tests, race checks, and evidence", anchor: hexxladb.Coord{Q: 0, R: 24}},
	{id: "product", label: "P", summary: "memory policy, provenance, and user intent", anchor: hexxladb.Coord{Q: -24, R: 24}},
}

type document struct {
	id      string
	topicID string
	ordinal int
	content string
	vector  []float32
}

type qualityMetrics struct {
	Cells                     int     `json:"cells"`
	NeighborhoodPrecision     float64 `json:"neighborhood_precision"`
	UsefulContentFraction     float64 `json:"useful_content_fraction"`
	SemanticPrecision         float64 `json:"semantic_precision"`
	SemanticLatticeDivergence float64 `json:"semantic_lattice_divergence"`
	MeanContextCells          float64 `json:"mean_context_cells"`
	MeanContextBytes          float64 `json:"mean_context_bytes"`
}

type relocationReport struct {
	OldCoordinatePreserved bool `json:"old_coordinate_preserved"`
	NewCoordinateCreated   bool `json:"new_coordinate_created"`
	SuccessorSubstituted   bool `json:"successor_substituted"`
}

type strategyReport struct {
	Strategy            string            `json:"strategy"`
	CollisionProbes     int               `json:"collision_probes"`
	Initial             qualityMetrics    `json:"initial"`
	AfterIncremental    qualityMetrics    `json:"after_incremental"`
	CoordinateStability float64           `json:"coordinate_stability"`
	SampleGrid          string            `json:"sample_grid"`
	Relocation          *relocationReport `json:"relocation,omitempty"`
}

type evidenceReport struct {
	SchemaVersion int `json:"schema_version"`
	Runtime       struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	} `json:"runtime"`
	Workload struct {
		Topics             int    `json:"topics"`
		DocumentsPerTopic  int    `json:"documents_per_topic"`
		InitialPerTopic    int    `json:"initial_per_topic"`
		NeighborhoodRadius int    `json:"neighborhood_radius"`
		MaxCells           int    `json:"max_cells"`
		SemanticK          int    `json:"semantic_k"`
		Seed               uint64 `json:"seed"`
	} `json:"workload"`
	Clustered   strategyReport `json:"clustered"`
	Interleaved strategyReport `json:"interleaved"`
}

type placementPolicy int

const (
	clustered placementPolicy = iota
	interleaved
)

type placementState struct {
	byID            map[string]hexxladb.Coord
	byCoord         map[hexxladb.Coord]document
	collisionProbes int
}

func main() {
	cfg := config{}
	flag.IntVar(&cfg.documentsPerTopic, "documents-per-topic", 20, "documents per topic (4..100)")
	flag.IntVar(&cfg.initialPerTopic, "initial-per-topic", 12, "documents placed before incremental insertion")
	flag.IntVar(&cfg.neighborhoodRadius, "neighborhood-radius", 2, "rings used for placement-quality evaluation (1..10)")
	flag.IntVar(&cfg.maxCells, "max-cells", 8, "maximum cells returned per context (1..256)")
	flag.IntVar(&cfg.semanticK, "semantic-k", 8, "semantic neighbors used for distribution comparison")
	flag.Uint64Var(&cfg.seed, "seed", 1, "deterministic corpus seed")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "lattice placement evidence: %v\n", err)
		os.Exit(2)
	}
	report, err := run(context.Background(), cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "lattice placement evidence: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "lattice placement evidence: encode report: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(cfg config) error {
	if cfg.documentsPerTopic < 4 || cfg.documentsPerTopic > maxDocumentsPerTopic {
		return fmt.Errorf("documents-per-topic must be between 4 and %d", maxDocumentsPerTopic)
	}
	if cfg.initialPerTopic < 1 || cfg.initialPerTopic >= cfg.documentsPerTopic {
		return errors.New("initial-per-topic must be positive and less than documents-per-topic")
	}
	if cfg.neighborhoodRadius < 1 || cfg.neighborhoodRadius > hexxladb.MaxRenderRadius {
		return fmt.Errorf("neighborhood-radius must be between 1 and %d", hexxladb.MaxRenderRadius)
	}
	if cfg.maxCells < 1 || cfg.maxCells > 256 {
		return errors.New("max-cells must be between 1 and 256")
	}
	if cfg.semanticK < 1 || cfg.semanticK >= cfg.documentsPerTopic {
		return errors.New("semantic-k must be positive and less than documents-per-topic")
	}
	return nil
}

func run(ctx context.Context, cfg config) (evidenceReport, error) {
	var report evidenceReport
	docs := generateCorpus(cfg)
	clusteredReport, err := runStrategy(ctx, cfg, docs, clustered)
	if err != nil {
		return report, fmt.Errorf("clustered strategy: %w", err)
	}
	interleavedReport, err := runStrategy(ctx, cfg, docs, interleaved)
	if err != nil {
		return report, fmt.Errorf("interleaved strategy: %w", err)
	}
	report.SchemaVersion = 1
	report.Runtime.GoVersion = runtime.Version()
	report.Runtime.GOOS = runtime.GOOS
	report.Runtime.GOARCH = runtime.GOARCH
	report.Workload.Topics = len(topics)
	report.Workload.DocumentsPerTopic = cfg.documentsPerTopic
	report.Workload.InitialPerTopic = cfg.initialPerTopic
	report.Workload.NeighborhoodRadius = cfg.neighborhoodRadius
	report.Workload.MaxCells = cfg.maxCells
	report.Workload.SemanticK = cfg.semanticK
	report.Workload.Seed = cfg.seed
	report.Clustered = clusteredReport
	report.Interleaved = interleavedReport
	if err := validateEvidence(report); err != nil {
		return evidenceReport{}, err
	}
	return report, nil
}

func runStrategy(ctx context.Context, cfg config, docs []document, policy placementPolicy) (strategyReport, error) {
	var report strategyReport
	tempDir, err := os.MkdirTemp("", "hexxladb-placement-evidence-")
	if err != nil {
		return report, fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	db, err := hexxladb.Open(filepath.Join(tempDir, "placement.db"), &hexxladb.Options{
		EmbeddingDimension: vectorDimension,
		DistanceMetric:     hexxladb.DistanceCosine,
	})
	if err != nil {
		return report, fmt.Errorf("open temporary database: %w", err)
	}
	defer func() { _ = db.Close() }()

	state := placementState{
		byID:    make(map[string]hexxladb.Coord, len(docs)+1),
		byCoord: make(map[hexxladb.Coord]document, len(docs)+1),
	}
	initial, incremental := splitCorpus(docs, cfg.initialPerTopic)
	if err := placeDocuments(ctx, db, &state, initial, policy); err != nil {
		return report, err
	}
	initialMetrics, err := evaluate(ctx, db, state.byCoord, cfg)
	if err != nil {
		return report, fmt.Errorf("evaluate initial placement: %w", err)
	}
	initialCoordinates := maps.Clone(state.byID)
	if err := placeDocuments(ctx, db, &state, incremental, policy); err != nil {
		return report, err
	}
	finalMetrics, err := evaluate(ctx, db, state.byCoord, cfg)
	if err != nil {
		return report, fmt.Errorf("evaluate incremental placement: %w", err)
	}
	stability, err := coordinateStability(db, initialCoordinates)
	if err != nil {
		return report, fmt.Errorf("verify coordinate stability: %w", err)
	}

	report.Strategy = policy.String()
	report.CollisionProbes = state.collisionProbes
	report.Initial = initialMetrics
	report.AfterIncremental = finalMetrics
	report.CoordinateStability = stability
	report.SampleGrid = sampleGrid(ctx, state.byCoord, policy)
	if policy == clustered {
		relocation, err := exerciseRelocation(ctx, db, &state, docs[0])
		if err != nil {
			return report, fmt.Errorf("exercise relocation: %w", err)
		}
		report.Relocation = new(relocation)
	}
	return report, nil
}

func (p placementPolicy) String() string {
	if p == clustered {
		return "topic_clustered"
	}
	return "intentionally_interleaved"
}

func splitCorpus(docs []document, initialPerTopic int) (initial, incremental []document) {
	for _, doc := range docs {
		if doc.ordinal < initialPerTopic {
			initial = append(initial, doc)
		} else {
			incremental = append(incremental, doc)
		}
	}
	return initial, incremental
}

func generateCorpus(cfg config) []document {
	rng := newGenerator(cfg.seed)
	docs := make([]document, 0, cfg.documentsPerTopic*len(topics))
	for ordinal := range cfg.documentsPerTopic {
		for topicIndex, topic := range topics {
			docs = append(docs, document{
				id:      fmt.Sprintf("%s-%03d", topic.id, ordinal),
				topicID: topic.id,
				ordinal: ordinal,
				content: fmt.Sprintf("%s evidence note %02d covers %s with deterministic representative detail.", topic.id, ordinal, topic.summary),
				vector:  topicVector(rng, topicIndex),
			})
		}
	}
	return docs
}

func topicVector(rng *generator, topicIndex int) []float32 {
	vector := make([]float32, vectorDimension)
	vector[topicIndex] = 1
	var norm float64
	for i := range vector {
		noise := (rng.float64()*2 - 1) * 0.04
		vector[i] += float32(noise)
		norm += float64(vector[i]) * float64(vector[i])
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= float32(norm)
	}
	return vector
}

func placeDocuments(ctx context.Context, db *hexxladb.DB, state *placementState, docs []document, policy placementPolicy) error {
	return db.Update(func(tx *hexxladb.Tx) error {
		for _, doc := range docs {
			anchor := hexxladb.Coord{}
			if policy == clustered {
				anchor = topicByID(doc.topicID).anchor
			}
			coord, probes, err := firstFreeCoordinate(tx, anchor)
			if err != nil {
				return fmt.Errorf("allocate %s: %w", doc.id, err)
			}
			state.collisionProbes += probes
			packed, err := hexxladb.Pack(coord)
			if err != nil {
				return fmt.Errorf("pack %s: %w", doc.id, err)
			}
			clusterHint := topicByID(doc.topicID).anchor
			clusterPacked, err := hexxladb.Pack(clusterHint)
			if err != nil {
				return fmt.Errorf("pack cluster hint %s: %w", doc.id, err)
			}
			record := hexxladb.CellRecord{
				Key:         packed,
				RawContent:  doc.content,
				Tags:        []string{"topic:" + doc.topicID, "document:" + doc.id},
				ClusterHint: new(clusterPacked),
				Provenance: hexxladb.ProvenanceWire{
					SourceID:   "placement-evidence/" + doc.topicID,
					Confidence: 0.9,
				},
			}
			if err := tx.PutCell(ctx, record); err != nil {
				return fmt.Errorf("put cell %s: %w", doc.id, err)
			}
			if err := tx.PutEmbedding(packed, doc.vector); err != nil {
				return fmt.Errorf("put embedding %s: %w", doc.id, err)
			}
			state.byID[doc.id] = coord
			state.byCoord[coord] = doc
		}
		return nil
	})
}

func firstFreeCoordinate(tx *hexxladb.Tx, anchor hexxladb.Coord) (hexxladb.Coord, int, error) {
	probes := 0
	for radius := range maxPlacementRadius + 1 {
		for _, candidate := range hexxladb.Ring(anchor, radius) {
			packed, err := hexxladb.Pack(candidate)
			if err != nil {
				return hexxladb.Coord{}, probes, err
			}
			_, occupied, err := tx.GetCell(packed)
			if err != nil {
				return hexxladb.Coord{}, probes, err
			}
			if !occupied {
				return candidate, probes, nil
			}
			probes++
		}
	}
	return hexxladb.Coord{}, probes, errors.New("no free coordinate within bounded placement radius")
}

func evaluate(ctx context.Context, db *hexxladb.DB, docsByCoord map[hexxladb.Coord]document, cfg config) (qualityMetrics, error) {
	var metrics qualityMetrics
	var relevantNeighbors, neighbors int
	var usefulBytes, contextBytes, contextCells int
	var semanticRelevant, semanticTotal int
	var divergence float64
	err := db.View(func(tx *hexxladb.Tx) error {
		coords := slices.SortedFunc(maps.Keys(docsByCoord), func(a, b hexxladb.Coord) int {
			if byQ := cmp.Compare(a.Q, b.Q); byQ != 0 {
				return byQ
			}
			return cmp.Compare(a.R, b.R)
		})
		for _, coord := range coords {
			doc := docsByCoord[coord]
			latticeTopics, localRelevant, localNeighbors, err := latticeTopicCounts(tx, coord, doc.topicID, cfg.neighborhoodRadius)
			if err != nil {
				return err
			}
			relevantNeighbors += localRelevant
			neighbors += localNeighbors

			pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
				Seeds:    []hexxladb.Coord{coord},
				MaxRing:  cfg.neighborhoodRadius,
				MaxCells: cfg.maxCells,
			})
			if err != nil {
				return err
			}
			contextCells += len(pack.Cells)
			for _, cell := range pack.Cells {
				contextBytes += len(cell.RawContent)
				if cellTopic(cell.Tags) == doc.topicID {
					usefulBytes += len(cell.RawContent)
				}
			}

			results, err := tx.SearchByEmbedding(doc.vector, hexxladb.EmbeddingSearchConfig{MaxResults: cfg.semanticK + 1})
			if err != nil {
				return err
			}
			semanticTopics := make(map[string]int)
			for _, result := range results {
				resultCoord, err := hexxladb.Unpack(result.Coord)
				if err != nil {
					return err
				}
				if resultCoord == coord {
					continue
				}
				resultDoc, ok := docsByCoord[resultCoord]
				if !ok {
					continue
				}
				semanticTopics[resultDoc.topicID]++
				semanticTotal++
				if resultDoc.topicID == doc.topicID {
					semanticRelevant++
				}
				if sumCounts(semanticTopics) == cfg.semanticK {
					break
				}
			}
			if got := sumCounts(semanticTopics); got != cfg.semanticK {
				return fmt.Errorf("semantic neighbors for %s: got %d, want %d", doc.id, got, cfg.semanticK)
			}
			divergence += topicDistributionDivergence(semanticTopics, latticeTopics)
		}
		return nil
	})
	if err != nil {
		return metrics, err
	}
	metrics.Cells = len(docsByCoord)
	metrics.NeighborhoodPrecision = ratio(relevantNeighbors, neighbors)
	metrics.UsefulContentFraction = ratio(usefulBytes, contextBytes)
	metrics.SemanticPrecision = ratio(semanticRelevant, semanticTotal)
	if metrics.Cells > 0 {
		metrics.SemanticLatticeDivergence = divergence / float64(metrics.Cells)
		metrics.MeanContextCells = float64(contextCells) / float64(metrics.Cells)
		metrics.MeanContextBytes = float64(contextBytes) / float64(metrics.Cells)
	}
	return metrics, nil
}

func latticeTopicCounts(tx *hexxladb.Tx, center hexxladb.Coord, topicID string, radius int) (map[string]int, int, int, error) {
	counts := make(map[string]int)
	relevant := 0
	total := 0
	for _, coord := range hexxladb.WalkRings(nil, center, radius) {
		if coord == center {
			continue
		}
		packed, err := hexxladb.Pack(coord)
		if err != nil {
			return nil, 0, 0, err
		}
		record, ok, err := tx.GetCell(packed)
		if err != nil {
			return nil, 0, 0, err
		}
		if !ok {
			continue
		}
		topic := cellTopic(record.Tags)
		counts[topic]++
		total++
		if topic == topicID {
			relevant++
		}
	}
	return counts, relevant, total, nil
}

func topicDistributionDivergence(a, b map[string]int) float64 {
	totalA := sumCounts(a)
	totalB := sumCounts(b)
	if totalA == 0 || totalB == 0 {
		return 1
	}
	var absoluteDifference float64
	for _, topic := range topics {
		pA := float64(a[topic.id]) / float64(totalA)
		pB := float64(b[topic.id]) / float64(totalB)
		absoluteDifference += math.Abs(pA - pB)
	}
	return absoluteDifference / 2
}

func sumCounts(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func coordinateStability(db *hexxladb.DB, initial map[string]hexxladb.Coord) (float64, error) {
	stable := 0
	err := db.View(func(tx *hexxladb.Tx) error {
		for id, coord := range initial {
			packed, err := hexxladb.Pack(coord)
			if err != nil {
				return err
			}
			record, ok, err := tx.GetCell(packed)
			if err != nil {
				return err
			}
			if ok && slices.Contains(record.Tags, "document:"+id) {
				stable++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return ratio(stable, len(initial)), nil
}

func exerciseRelocation(ctx context.Context, db *hexxladb.DB, state *placementState, old document) (relocationReport, error) {
	var report relocationReport
	oldCoord := state.byID[old.id]
	successor := old
	successor.id += "-v2"
	successor.content += " This successor records an application-requested relocation."
	var newCoord hexxladb.Coord
	err := db.Update(func(tx *hexxladb.Tx) error {
		var probes int
		var err error
		newCoord, probes, err = firstFreeCoordinate(tx, topicByID(old.topicID).anchor)
		if err != nil {
			return err
		}
		state.collisionProbes += probes
		packed, err := hexxladb.Pack(newCoord)
		if err != nil {
			return err
		}
		hint := topicByID(old.topicID).anchor
		hintPacked, err := hexxladb.Pack(hint)
		if err != nil {
			return err
		}
		if err := tx.PutCell(ctx, hexxladb.CellRecord{
			Key:         packed,
			RawContent:  successor.content,
			Tags:        []string{"topic:" + successor.topicID, "document:" + successor.id},
			ClusterHint: new(hintPacked),
			Provenance: hexxladb.ProvenanceWire{
				SourceID:   "placement-evidence/" + successor.topicID,
				Confidence: 0.95,
			},
		}); err != nil {
			return err
		}
		if err := tx.PutEmbedding(packed, successor.vector); err != nil {
			return err
		}
		return tx.MarkSupersedes(newCoord, oldCoord, "application-requested stable relocation")
	})
	if err != nil {
		return report, err
	}
	err = db.View(func(tx *hexxladb.Tx) error {
		oldPacked, err := hexxladb.Pack(oldCoord)
		if err != nil {
			return err
		}
		newPacked, err := hexxladb.Pack(newCoord)
		if err != nil {
			return err
		}
		_, report.OldCoordinatePreserved, err = tx.GetCell(oldPacked)
		if err != nil {
			return err
		}
		_, report.NewCoordinateCreated, err = tx.GetCell(newPacked)
		if err != nil {
			return err
		}
		pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
			Seeds:    []hexxladb.Coord{oldCoord},
			MaxRing:  0,
			MaxCells: 16,
			Assembly: hexxladb.ContextAssemblyConfig{
				FilterSuperseded: true,
			},
		})
		if err != nil {
			return err
		}
		report.SuccessorSubstituted = slices.ContainsFunc(pack.Cells, func(cell hexxladb.CellView) bool {
			return cell.Coord == newCoord &&
				cell.SupersededFrom != nil &&
				*cell.SupersededFrom == oldCoord
		})
		return nil
	})
	return report, err
}

func sampleGrid(ctx context.Context, docsByCoord map[hexxladb.Coord]document, policy placementPolicy) string {
	center := hexxladb.Coord{}
	if policy == clustered {
		center = topics[0].anchor
	}
	return hexxladb.RenderHexGrid(ctx, center, 2, func(coord hexxladb.Coord) string {
		doc, ok := docsByCoord[coord]
		if !ok {
			return "."
		}
		return topicByID(doc.topicID).label
	})
}

func cellTopic(tags []string) string {
	for _, tag := range tags {
		if topic, ok := strings.CutPrefix(tag, "topic:"); ok {
			return topic
		}
	}
	return ""
}

func topicByID(id string) topic {
	for _, topic := range topics {
		if topic.id == id {
			return topic
		}
	}
	panic("unknown evidence topic: " + id)
}

func validateEvidence(report evidenceReport) error {
	good := report.Clustered.AfterIncremental
	poor := report.Interleaved.AfterIncremental
	switch {
	case report.Clustered.CoordinateStability != 1 || report.Interleaved.CoordinateStability != 1:
		return errors.New("existing coordinates changed during incremental insertion")
	case good.NeighborhoodPrecision < 0.8:
		return fmt.Errorf("clustered neighborhood precision %.3f is below 0.8", good.NeighborhoodPrecision)
	case poor.NeighborhoodPrecision > 0.4:
		return fmt.Errorf("interleaved neighborhood precision %.3f is above 0.4", poor.NeighborhoodPrecision)
	case good.UsefulContentFraction-poor.UsefulContentFraction < 0.4:
		return errors.New("placement strategies do not separate useful content fraction by at least 0.4")
	case good.SemanticPrecision < 0.8 || poor.SemanticPrecision < 0.8:
		return errors.New("synthetic semantic retrieval precision is below 0.8")
	case good.SemanticLatticeDivergence > 0.25:
		return fmt.Errorf("clustered semantic/lattice divergence %.3f is above 0.25", good.SemanticLatticeDivergence)
	case poor.SemanticLatticeDivergence < 0.5:
		return fmt.Errorf("interleaved semantic/lattice divergence %.3f is below 0.5", poor.SemanticLatticeDivergence)
	case report.Clustered.Relocation == nil ||
		!report.Clustered.Relocation.OldCoordinatePreserved ||
		!report.Clustered.Relocation.NewCoordinateCreated ||
		!report.Clustered.Relocation.SuccessorSubstituted:
		return errors.New("relocation/supersession evidence failed")
	default:
		return nil
	}
}

type generator struct{ state uint64 }

func newGenerator(seed uint64) *generator { return &generator{state: seed} }

func (g *generator) float64() float64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return float64(g.state>>11) / float64(uint64(1)<<53)
}
