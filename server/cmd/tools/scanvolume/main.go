// Command scanvolume measures how many stokens the Presidio risk scanner
// reads and meters per agent tool response, and what each volume-reduction
// option would change.
//
// It runs the production preparation and counting code — PrepareScanText from
// the risk_analysis package (JSON-to-YAML reformat plus the 50 KiB cap) and
// the o200k_base stokens codec — over tool-response payloads reconstructed
// from real files, so the token numbers are the ones the scanner and the
// billing ledger would actually see.
//
// Usage:
//
//	go run ./cmd/tools/scanvolume -root .. -files 400 -edits 8
package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"flag"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "scanvolume: %v\n", err)
		os.Exit(1)
	}
}

type config struct {
	root      string
	files     int
	edits     int
	minBytes  int
	maxBytes  int
	seed      uint64
	csvOutput bool
}

func run() error {
	cfg := config{
		root:      ".",
		files:     0,
		edits:     0,
		minBytes:  0,
		maxBytes:  0,
		seed:      0,
		csvOutput: false,
	}
	flag.StringVar(&cfg.root, "root", ".", "directory to sample source files from")
	flag.IntVar(&cfg.files, "files", 400, "number of files to sample into the corpus")
	flag.IntVar(&cfg.edits, "edits", 8, "edits per file in the session simulation")
	flag.IntVar(&cfg.minBytes, "min-bytes", 256, "skip files smaller than this")
	flag.IntVar(&cfg.maxBytes, "max-bytes", 4<<20, "skip files larger than this")
	flag.Uint64Var(&cfg.seed, "seed", 723, "sampling seed")
	flag.BoolVar(&cfg.csvOutput, "csv", false, "emit CSV instead of markdown")
	flag.Parse()

	if cfg.files <= 0 || cfg.edits <= 0 {
		return fmt.Errorf("files and edits must be positive")
	}

	corpus, err := loadCorpus(cfg)
	if err != nil {
		return err
	}
	if len(corpus) == 0 {
		return fmt.Errorf("no files matched under %s", cfg.root)
	}

	m := newMeter()
	report, err := analyze(context.Background(), m, cfg, corpus)
	if err != nil {
		return err
	}

	if cfg.csvOutput {
		return report.writeCSV(os.Stdout)
	}
	if _, err := os.Stdout.WriteString(report.markdown(cfg)); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- corpus

type corpusFile struct {
	path string
	body string
}

var corpusExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".py": true,
	".sql": true, ".md": true, ".json": true, ".yaml": true, ".yml": true,
	".sh": true, ".proto": true, ".css": true, ".html": true,
}

var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	".venv": true, "vendor": true, ".playwright-cli": true, ".mise": true,
}

func loadCorpus(cfg config) ([]corpusFile, error) {
	var paths []string
	err := filepath.WalkDir(cfg.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable subtrees are not worth failing the sample
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !corpusExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil || info.Size() < int64(cfg.minBytes) || info.Size() > int64(cfg.maxBytes) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk corpus root: %w", err)
	}

	sort.Strings(paths)
	rng := rand.New(rand.NewPCG(cfg.seed, cfg.seed)) // #nosec G404 -- reproducible sampling, not security
	rng.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
	if len(paths) > cfg.files {
		paths = paths[:cfg.files]
	}
	sort.Strings(paths)

	out := make([]corpusFile, 0, len(paths))
	for _, path := range paths {
		body, readErr := os.ReadFile(path) // #nosec G304 -- operator-supplied corpus root
		if readErr != nil || !isText(body) {
			continue
		}
		out = append(out, corpusFile{path: path, body: string(body)})
	}
	return out, nil
}

func isText(b []byte) bool {
	return !slices.Contains(b, 0)
}

// ---------------------------------------------------------------- metering

// meter reproduces what each lane reads and meters for one stored message.
type meter struct {
	codec *stokens.Codec
}

func newMeter() *meter {
	return &meter{codec: stokens.NewCodec()}
}

// laneCost is the per-message cost of one stored tool response.
type laneCost struct {
	bytes int

	// asyncScanned is what the pystreams Presidio consumer reads: the raw
	// stored content, with no reformat and no cap. It is also the value the
	// shadow_stream meter reading carries, because publishPresidioScanRequests
	// counts msg.scanSurface() before any preparation.
	asyncScanned int

	// normalizedScanned is the same content after the JSON-to-YAML reformat
	// but without the cap. The gap to asyncScanned is what the async lane
	// pays for skipping a normalization that changes no semantics.
	normalizedScanned int

	// inlineScanned is what the Go PresidioClient sends to /analyze after
	// PrepareScanText.
	inlineScanned int

	// inlineMetered is the inline_batch ledger value. analyzeOne marks a
	// truncated result not Completed and recordBatchResults skips metering
	// for it, so oversized messages contribute nothing on this lane.
	inlineMetered int

	truncated bool
}

func (m *meter) cost(ctx context.Context, content string) (laneCost, error) {
	raw, err := m.codec.Count(ctx, content)
	if err != nil {
		return laneCost{}, fmt.Errorf("count raw content: %w", err)
	}
	normalized := risk_analysis.NormalizeScanText(content)
	normalizedCount, err := m.codec.Count(ctx, normalized)
	if err != nil {
		return laneCost{}, fmt.Errorf("count normalized content: %w", err)
	}
	prepared, truncated := risk_analysis.PrepareScanText(content)
	preparedCount, err := m.codec.Count(ctx, prepared)
	if err != nil {
		return laneCost{}, fmt.Errorf("count prepared content: %w", err)
	}
	cost := laneCost{
		bytes:             len(content),
		asyncScanned:      raw,
		normalizedScanned: normalizedCount,
		inlineScanned:     preparedCount,
		inlineMetered:     preparedCount,
		truncated:         truncated,
	}
	if truncated {
		cost.inlineMetered = 0
	}
	return cost, nil
}

// ---------------------------------------------------------------- analysis

type shapeStats struct {
	shape string
	n     int

	byteSizes []int

	sumBytes         int64
	sumAsyncScanned  int64
	sumNormalized    int64
	sumInlineScanned int64
	sumInlineMetered int64
	truncated        int

	// novel* are the same measurements taken after dropping the field that
	// only restates already-scanned repository state.
	sumNovelBytes       int64
	sumNovelAsync       int64
	sumNovelInlineScan  int64
	sumNovelInlineMeter int64
	novelTruncated      int
}

func (s *shapeStats) add(cost laneCost, novel laneCost) {
	s.n++
	s.byteSizes = append(s.byteSizes, cost.bytes)
	s.sumBytes += int64(cost.bytes)
	s.sumAsyncScanned += int64(cost.asyncScanned)
	s.sumNormalized += int64(cost.normalizedScanned)
	s.sumInlineScanned += int64(cost.inlineScanned)
	s.sumInlineMetered += int64(cost.inlineMetered)
	if cost.truncated {
		s.truncated++
	}
	s.sumNovelBytes += int64(novel.bytes)
	s.sumNovelAsync += int64(novel.asyncScanned)
	s.sumNovelInlineScan += int64(novel.inlineScanned)
	s.sumNovelInlineMeter += int64(novel.inlineMetered)
	if novel.truncated {
		s.novelTruncated++
	}
}

// echoesRepositoryState reports whether the shape carries a field that only
// restates content an earlier message already carried.
func (s *shapeStats) echoesRepositoryState() bool {
	return s.shape == shapeEdit || s.shape == shapeMultiEdit
}

func (s *shapeStats) overCap() int {
	count := 0
	for _, size := range s.byteSizes {
		if size > risk_analysis.PresidioMaxMessageBytes {
			count++
		}
	}
	return count
}

// sessionStats models one chat per file: a Read followed by cfg.edits Edits,
// which is the shape of an agent editing pass and the case the ticket calls
// out as repeated content.
type sessionStats struct {
	messages int

	// today is the full scanner read volume across both lanes.
	today int64

	// dropEchoed removes originalFile / originalFileContents from the edit
	// family before scanning.
	dropEchoed int64

	// hashWholeMessage skips a message whose exact content was already
	// scanned in this chat.
	hashWholeMessage int64

	// hashEchoedField scans the echoed field only the first time its hash is
	// seen in this chat, and the rest of the message every time.
	hashEchoedField int64
}

type report struct {
	corpusFiles int
	corpusBytes int64
	shapes      []*shapeStats
	session     sessionStats
	editsPerRun int
}

func analyze(ctx context.Context, m *meter, cfg config, corpus []corpusFile) (*report, error) {
	rep := &report{
		corpusFiles: len(corpus),
		corpusBytes: 0,
		shapes:      nil,
		session:     sessionStats{messages: 0, today: 0, dropEchoed: 0, hashWholeMessage: 0, hashEchoedField: 0},
		editsPerRun: cfg.edits,
	}
	byShape := map[string]*shapeStats{}
	for _, shape := range allShapes {
		byShape[shape] = &shapeStats{
			shape:               shape,
			n:                   0,
			byteSizes:           nil,
			sumBytes:            0,
			sumAsyncScanned:     0,
			sumNormalized:       0,
			sumInlineScanned:    0,
			sumInlineMetered:    0,
			truncated:           0,
			sumNovelBytes:       0,
			sumNovelAsync:       0,
			sumNovelInlineScan:  0,
			sumNovelInlineMeter: 0,
			novelTruncated:      0,
		}
	}

	for _, file := range corpus {
		rep.corpusBytes += int64(len(file.body))

		samples := []sample{
			buildRead(file.path, file.body),
			buildEdit(file.path, file.body, 0),
			buildMultiEdit(file.path, file.body, 0),
			buildWrite(file.path, file.body),
			buildGrep(file.path, file.body),
			buildBash(file.path, file.body),
			buildMCP(file.path, file.body),
		}
		for _, s := range samples {
			cost, err := m.cost(ctx, s.content)
			if err != nil {
				return nil, err
			}
			novel, err := m.cost(ctx, s.novel)
			if err != nil {
				return nil, err
			}
			byShape[s.shape].add(cost, novel)
		}

		if err := rep.session.addRun(ctx, m, file, cfg.edits); err != nil {
			return nil, err
		}
	}

	for _, shape := range allShapes {
		rep.shapes = append(rep.shapes, byShape[shape])
	}
	return rep, nil
}

// addRun simulates one chat: the agent reads the file, then edits it
// cfg.edits times. Every Edit response restates the whole pre-edit file.
func (s *sessionStats) addRun(ctx context.Context, m *meter, file corpusFile, edits int) error {
	seenMessage := map[[32]byte]bool{}
	seenEchoed := map[[32]byte]bool{}

	samples := make([]sample, 0, edits+1)
	samples = append(samples, buildRead(file.path, file.body))
	for i := range edits {
		samples = append(samples, buildEdit(file.path, file.body, i))
	}

	for _, sm := range samples {
		s.messages++

		cost, err := m.cost(ctx, sm.content)
		if err != nil {
			return err
		}
		bothLanes := int64(cost.asyncScanned + cost.inlineScanned)
		s.today += bothLanes

		novel, err := m.cost(ctx, sm.novel)
		if err != nil {
			return err
		}
		s.dropEchoed += int64(novel.asyncScanned + novel.inlineScanned)

		messageKey := sha256.Sum256([]byte(sm.content))
		if !seenMessage[messageKey] {
			seenMessage[messageKey] = true
			s.hashWholeMessage += bothLanes
		}

		s.hashEchoedField += int64(novel.asyncScanned + novel.inlineScanned)
		if sm.echoed != "" {
			echoedKey := sha256.Sum256([]byte(sm.echoed))
			if !seenEchoed[echoedKey] {
				seenEchoed[echoedKey] = true
				echoed, err := m.cost(ctx, sm.echoed)
				if err != nil {
					return err
				}
				s.hashEchoedField += int64(echoed.asyncScanned + echoed.inlineScanned)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- reporting

func percentile(sorted []int, p float64) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func pct(part, whole int64) string {
	if whole == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

func kib(b int) string { return fmt.Sprintf("%.1f", float64(b)/1024) }

func thousands(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

type totals struct {
	async       int64
	normalized  int64
	inline      int64
	metered     int64
	novelAsync  int64
	novelInline int64
}

func (r *report) totals() totals {
	var t totals
	for _, s := range r.shapes {
		t.async += s.sumAsyncScanned
		t.normalized += s.sumNormalized
		t.inline += s.sumInlineScanned
		t.metered += s.sumInlineMetered
		t.novelAsync += s.sumNovelAsync
		t.novelInline += s.sumNovelInlineScan
	}
	return t
}

func (r *report) markdown(cfg config) string {
	var out strings.Builder
	// strings.Builder never fails, so the report is assembled in memory and
	// written once with a checked error at the call site.
	line := func(format string, a ...any) { fmt.Fprintf(&out, format, a...) }

	line("## Corpus\n\n")
	line("%d files, %s bytes of source text sampled from `%s` (seed %d). "+
		"Each file is turned into one tool response of every shape, so every row below spans the same %d files.\n\n",
		r.corpusFiles, thousands(r.corpusBytes), cfg.root, cfg.seed, r.corpusFiles)

	line("## Per-shape response size and scanner cost\n\n")
	line("`raw` is the stored content the pystreams consumer reads and the `shadow_stream` ledger records. " +
		"`normalized` is the same content after the JSON-to-YAML reformat, uncapped. " +
		"`inline` is what the Go client sends to `/analyze`: normalized and capped at 50 KiB. " +
		"`metered` is the `inline_batch` ledger value, zero for a truncated message.\n\n")
	line("| Shape | n | p50 KiB | p90 KiB | max KiB | >50 KiB | raw stokens | normalized | inline | metered | ledger total |\n")
	line("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, s := range r.shapes {
		sizes := slices.Clone(s.byteSizes)
		sort.Ints(sizes)
		line("| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			s.shape, s.n,
			kib(percentile(sizes, 0.50)), kib(percentile(sizes, 0.90)), kib(percentile(sizes, 1.0)),
			pct(int64(s.overCap()), int64(s.n)),
			thousands(s.sumAsyncScanned), thousands(s.sumNormalized),
			thousands(s.sumInlineScanned), thousands(s.sumInlineMetered),
			thousands(s.sumAsyncScanned+s.sumInlineMetered))
	}

	t := r.totals()
	ledger := t.async + t.metered
	scanned := t.async + t.inline

	line("\n## Where the ledger volume sits\n\n")
	line("| Lane | stokens | share of ledger |\n| --- | ---: | ---: |\n")
	line("| `shadow_stream` (raw, uncapped) | %s | %s |\n", thousands(t.async), pct(t.async, ledger))
	line("| `inline_batch` (normalized, capped, oversized not metered) | %s | %s |\n", thousands(t.metered), pct(t.metered, ledger))
	line("| **ledger total** | **%s** | |\n", thousands(ledger))
	line("\nScanner read volume across both lanes is %s stokens; the ledger records %s of it, because a message "+
		"over the cap is scanned inline but marked not completed and therefore never metered on that lane. "+
		"Skipping the JSON-to-YAML reformat costs the async lane %s stokens against the %s it would spend normalized, "+
		"a %.2fx multiplier on identical content.\n",
		thousands(scanned), pct(ledger, scanned),
		thousands(t.async), thousands(t.normalized), float64(t.async)/float64(max64(t.normalized, 1)))

	line("\n## Cost of the field that restates already-scanned state\n\n")
	line("| Shape | scanned stokens today | without originalFile | change |\n| --- | ---: | ---: | ---: |\n")
	for _, s := range r.shapes {
		if !s.echoesRepositoryState() {
			continue
		}
		today := s.sumAsyncScanned + s.sumInlineScanned
		without := s.sumNovelAsync + s.sumNovelInlineScan
		line("| %s | %s | %s | %s |\n", s.shape, thousands(today), thousands(without), delta(without, today))
	}

	line("\n## Option modelling on the same corpus\n\n")
	line("Scanned stokens, not ledger stokens: the ledger has its own defect (a truncated message is scanned but not " +
		"metered inline) and modelling both at once conflates scanner cost with an accounting fix. Shape mix here is uniform — " +
		"one response of every shape per file — which is not the production mix. Read these as the per-unit effect of each " +
		"lever and weight them with the ClickHouse breakdown before forecasting a bill.\n\n")
	line("| Option | scanned stokens | change |\n| --- | ---: | ---: |\n")
	type option struct {
		name    string
		scanned int64
	}
	options := []option{
		{"today", scanned},
		{"normalize the async lane, no cap change", t.normalized + t.inline},
		{"cap both lanes at 50 KiB", t.inline * 2},
		{"one lane only, async as it is today", t.async},
		{"one lane only, normalized and capped", t.inline},
		{"drop originalFile from the edit family, both lanes as today", t.novelAsync + t.novelInline},
		{"one normalized capped lane and no originalFile", r.novelInlineOnly()},
	}
	for _, o := range options {
		line("| %s | %s | %s |\n", o.name, thousands(o.scanned), delta(o.scanned, scanned))
	}
	line("\nThe levers compose, so the last row is the floor this corpus reaches without dropping a surface.\n")

	line("\n## Repeated content in an editing session\n\n")
	line("One chat per file: a Read followed by %d Edits of the same file, %d messages total.\n\n", r.editsPerRun, r.session.messages)
	line("| Strategy | scanned stokens | change |\n| --- | ---: | ---: |\n")
	line("| today | %s | |\n", thousands(r.session.today))
	line("| skip messages whose whole content was already scanned | %s | %s |\n",
		thousands(r.session.hashWholeMessage), delta(r.session.hashWholeMessage, r.session.today))
	line("| scan each distinct originalFile once per chat | %s | %s |\n",
		thousands(r.session.hashEchoedField), delta(r.session.hashEchoedField, r.session.today))
	line("| drop originalFile outright | %s | %s |\n",
		thousands(r.session.dropEchoed), delta(r.session.dropEchoed, r.session.today))

	return out.String()
}

// novelInlineOnly is the corpus cost of running a single normalized, capped
// lane over responses whose originalFile has been dropped.
func (r *report) novelInlineOnly() int64 {
	var sum int64
	for _, s := range r.shapes {
		sum += s.sumNovelInlineScan
	}
	return sum
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func delta(got, base int64) string {
	if base == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", 100*float64(got-base)/float64(base))
}

func (r *report) writeCSV(w *os.File) error {
	out := csv.NewWriter(w)
	defer out.Flush()
	if err := out.Write([]string{
		"shape", "n", "sum_bytes", "over_cap", "async_stokens", "inline_stokens",
		"inline_metered_stokens", "novel_async_stokens", "novel_inline_stokens",
	}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, s := range r.shapes {
		if err := out.Write([]string{
			s.shape,
			strconv.Itoa(s.n),
			strconv.FormatInt(s.sumBytes, 10),
			strconv.Itoa(s.overCap()),
			strconv.FormatInt(s.sumAsyncScanned, 10),
			strconv.FormatInt(s.sumInlineScanned, 10),
			strconv.FormatInt(s.sumInlineMetered, 10),
			strconv.FormatInt(s.sumNovelAsync, 10),
			strconv.FormatInt(s.sumNovelInlineScan, 10),
		}); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	return nil
}
