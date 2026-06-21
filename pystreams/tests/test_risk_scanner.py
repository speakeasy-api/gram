from pystreams.risk.scanner import Recognized, ThreadScanner


class _Result:
    """Minimal stand-in for a Presidio ``RecognizerResult``."""

    def __init__(
        self, entity_type: str, start: int = 0, end: int = 0, score: float = 0.5
    ):
        self.entity_type = entity_type
        self.start = start
        self.end = end
        self.score = score


class FakeAnalyzer:
    """Records calls and returns canned detections keyed by input text.

    Lets the scanner tests run without loading Presidio's NLP model and keeps
    detection results deterministic.
    """

    def __init__(self, detections: dict[str, list[Recognized]] | None = None):
        self.detections = detections or {}
        self.calls: list[tuple[str, list[str] | None]] = []

    def analyze(
        self, *, text: str, entities: list[str] | None, language: str
    ) -> list[Recognized]:
        self.calls.append((text, entities))
        assert language == "en"
        return self.detections.get(text, [])


def _scanner(analyzer: FakeAnalyzer) -> ThreadScanner:
    return ThreadScanner(analyzer)


async def test_maps_recognizer_results_to_detections():
    content = "email me at a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=12, end=19, score=0.85)]}
    )

    (detection,) = await _scanner(analyzer).scan(content, None)

    assert detection.entity_type == "EMAIL_ADDRESS"
    assert detection.match == "a@b.com"
    assert detection.start_pos == 12
    assert detection.end_pos == 19
    assert detection.confidence == 0.85


async def test_returns_one_detection_per_recognized_span():
    content = "call 555-0100 or 555-0199"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("PHONE_NUMBER", start=5, end=13),
                _Result("PHONE_NUMBER", start=17, end=25),
            ]
        }
    )

    detections = await _scanner(analyzer).scan(content, None)

    assert [d.match for d in detections] == ["555-0100", "555-0199"]


async def test_byte_offsets_for_multibyte_content():
    # "café " is 5 characters but 6 UTF-8 bytes; a match after it must be
    # reported in byte positions, not character positions.
    content = "café a@b.com"
    analyzer = FakeAnalyzer(
        {content: [_Result("EMAIL_ADDRESS", start=5, end=12, score=0.9)]}
    )

    (detection,) = await _scanner(analyzer).scan(content, None)

    assert detection.match == "a@b.com"
    assert detection.start_pos == 6  # one extra byte from the 'é'
    assert detection.end_pos == 13


async def test_false_positives_are_filtered():
    # "10.0.0.1" is RFC1918 space and is dropped; "a@b.com" is a real match.
    content = "10.0.0.1 and a@b.com"
    analyzer = FakeAnalyzer(
        {
            content: [
                _Result("IP_ADDRESS", start=0, end=8),
                _Result("EMAIL_ADDRESS", start=13, end=20, score=0.7),
            ]
        }
    )

    detections = await _scanner(analyzer).scan(content, None)

    # Only the real match survives; the reserved IP is dropped at the scanner.
    (detection,) = detections
    assert detection.entity_type == "EMAIL_ADDRESS"
    assert detection.match == "a@b.com"


async def test_all_false_positives_yields_no_detections():
    content = "reach me at user@example.com"
    analyzer = FakeAnalyzer({content: [_Result("EMAIL_ADDRESS", start=12, end=28)]})

    # example.com is a placeholder domain: filtered out entirely.
    assert await _scanner(analyzer).scan(content, None) == []


async def test_nothing_recognized_yields_no_detections():
    analyzer = FakeAnalyzer()  # recognizes nothing

    assert await _scanner(analyzer).scan("nothing sensitive here", None) == []


async def test_requested_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})

    await _scanner(analyzer).scan("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])

    # The explicit request set is passed through to the analyzer verbatim.
    assert analyzer.calls == [("a@b.com", ["EMAIL_ADDRESS", "PHONE_NUMBER"])]


async def test_none_entities_forwarded_to_analyzer():
    analyzer = FakeAnalyzer({"a@b.com": [_Result("EMAIL_ADDRESS", start=0, end=7)]})

    await _scanner(analyzer).scan("a@b.com", None)

    # None tells Presidio to scan every type; it is forwarded unchanged.
    assert analyzer.calls == [("a@b.com", None)]
