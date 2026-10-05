# ADR-0065: one recursive strategy, about 500 tokens estimated from characters (ADR-0071), no overlap, and a paragraph that fits
# stays whole.

from dataclasses import replace

import pytest

from infra.rules import RulesFile, RulesFileError
from knowledge import chunking
from knowledge.chunking import ChunkingRules, split

SMALL = ChunkingRules(
    version=1,
    max_tokens=10,
    chars_per_token=4,
    separators=("\n\n\n", "\n\n", "\n", ". ", " ", ""),
    pdf_text="layout",
    layout_min_ratio=0.9,
)


def test_the_rules_are_the_ones_adr_0065_declares() -> None:
    rules = chunking.rules()
    assert (rules.max_tokens, rules.chars_per_token) == (500, 4)
    assert (rules.version, rules.pdf_text) == (2, "layout"), "ADR-0076 changed the text that is cut, and with it the version"
    assert rules.layout_min_ratio == 0.9
    assert rules.separators[0] == "\n\n\n" and rules.separators[-1] == ""
    assert rules.estimate_tokens("a" * 9) == 3, "rounded up: a started token is a token"


def test_a_text_that_fits_is_one_chunk() -> None:
    assert split("  Depto 2 ambientes.  ", rules=SMALL) == ["Depto 2 ambientes."]
    assert split("   \n\n  ", rules=SMALL) == []


def test_the_largest_separator_that_fits_is_the_one_used() -> None:
    first, second = "Primer parrafo corto.", "Segundo parrafo corto."
    assert split(f"{first}\n\n{second}", rules=SMALL) == [first, second]

    # Two short paragraphs that fit together stay together: the cut is the need, not the separator.
    roomy = replace(SMALL, max_tokens=100)
    assert split(f"{first}\n\n{second}", rules=roomy) == [f"{first}\n\n{second}"]


def test_every_chunk_fits_and_no_text_is_lost() -> None:
    text = "Requisitos para alquilar: recibo de sueldo, garantia propietaria o seguro de caucion. " * 30
    chunks = split(text, rules=SMALL)
    assert all(SMALL.estimate_tokens(chunk) <= SMALL.max_tokens for chunk in chunks)
    assert "".join(chunks).replace(" ", "") == text.replace(" ", "")


def test_the_pieces_of_a_cut_paragraph_join_their_neighbours_instead_of_staying_tiny() -> None:
    long = "Primera oracion larga. Segunda oracion larga. Tercera."
    short = "Corto."
    # The tail of the long paragraph takes the short one after it...
    assert split(f"{long}\n\n{short}", rules=SMALL) == [
        "Primera oracion larga.",
        f"Segunda oracion larga. Tercera.\n\n{short}",
    ]
    # ...and the short one before it takes the head of the long one.
    assert split(f"{short}\n\n{long}", rules=SMALL) == [
        f"{short}\n\nPrimera oracion larga.",
        "Segunda oracion larga. Tercera.",
    ]


def test_a_run_without_any_separator_is_cut_by_characters() -> None:
    chunks = split("x" * 95, rules=SMALL)
    assert [len(c) for c in chunks] == [40, 40, 15]


@pytest.mark.parametrize(
    ("raw", "message"),
    [
        ({"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 50, "separators": [""]}, "overlap"),
        ({"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 0, "separators": "\n"}, "separators"),
        ({"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 0, "separators": ["\n"]}, "empty string"),
        ({"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 0, "separators": [""], "pdf_text": "ocr"}, "pdf_text"),
        ({"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 0, "separators": [""]}, "pdf_text"),
        *(
            (
                {"max_tokens": 500, "chars_per_token": 4, "overlap_tokens": 0, "separators": [""], "pdf_text": "layout", **ratio},
                "layout_min_ratio",
            )
            for ratio in ({}, {"layout_min_ratio": 0}, {"layout_min_ratio": 1.5}, {"layout_min_ratio": "0.9"})
        ),
    ],
)
def test_a_file_that_asks_for_what_the_splitter_cannot_do_stops_the_worker(
    monkeypatch: pytest.MonkeyPatch, raw: dict, message: str
) -> None:
    monkeypatch.setattr(chunking.rules_file, "load", lambda name: RulesFile(name=name, version=1, raw=raw))
    chunking.rules.cache_clear()
    try:
        with pytest.raises(RulesFileError, match=message):
            chunking.rules()
    finally:
        chunking.rules.cache_clear()
