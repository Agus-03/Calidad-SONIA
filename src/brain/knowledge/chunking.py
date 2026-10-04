# The chunking of F06 (ADR-0065): one strategy, the recursive one, with its parameters in rules/chunking.yaml. A function and not
# an interface while it is the only strategy (regla 7 of the ADR).

import math
from collections.abc import Callable
from dataclasses import dataclass
from functools import lru_cache

from infra import rules as rules_file


@dataclass(frozen=True)
class ChunkingRules:
    # What document_chunks.chunking_version stores (ADR-0065, regla 9).
    version: int
    max_tokens: int
    # The equivalence the tokens are estimated with (ADR-0071).
    chars_per_token: int
    separators: tuple[str, ...]

    # What document_chunks.token_count stores: an estimate, from the same equivalence the cut was made with.
    def estimate_tokens(self, text: str) -> int:
        return math.ceil(len(text) / self.chars_per_token)


@lru_cache(maxsize=1)
def rules() -> ChunkingRules:
    loaded = rules_file.load("chunking.yaml")
    overlap = loaded.integer("overlap_tokens", minimum=0)
    # The parameter exists so the evaluation can raise it (ADR-0065, regla 4); the splitter does not overlap yet, and a file that
    # asks for it must stop the worker instead of being silently ignored.
    if overlap != 0:
        raise rules_file.RulesFileError("rules: chunking.yaml asks for an overlap the splitter does not implement yet")
    separators = loaded.raw.get("separators")
    if not isinstance(separators, list) or not all(isinstance(s, str) for s in separators) or not separators:
        raise rules_file.RulesFileError("rules: chunking.yaml has no separators list")
    if separators[-1] != "":
        raise rules_file.RulesFileError("rules: chunking.yaml has to end its separators with the empty string, the last resort")
    return ChunkingRules(
        version=loaded.version,
        max_tokens=loaded.integer("max_tokens", minimum=1),
        chars_per_token=loaded.integer("chars_per_token", minimum=1),
        separators=tuple(separators),
    )


# split cuts text into pieces of at most max_tokens, estimated from characters, trying the separators from the largest unit to the
# smallest. A piece is cut at a smaller separator only when it does not fit whole: a paragraph that fits stays one chunk.
def split(text: str, *, rules: ChunkingRules) -> list[str]:
    pieces = _split(text, rules.separators, rules.estimate_tokens, rules.max_tokens)
    return [piece.strip() for piece in pieces if piece.strip()]


def _split(text: str, separators: tuple[str, ...], count: Callable[[str], int], limit: int) -> list[str]:
    if count(text) <= limit:
        return [text]

    position = next(i for i, sep in enumerate(separators) if sep == "" or sep in text)
    separator, smaller = separators[position], separators[position + 1 :]
    if separator == "":
        return _by_characters(text, count, limit)

    # The separator stays at the end of the part before it, so a cut after ". " keeps the period and no text is lost at a cut.
    parts = text.split(separator)
    parts = [part + separator for part in parts[:-1]] + parts[-1:]

    # A part too large is cut at the smaller separators, and its pieces join the accumulation like any other part: the head of a
    # long paragraph fills what the one before it left, and its tail takes the next one, so neither is left as a tiny chunk.
    chunks: list[str] = []
    current = ""
    for part in parts:
        pieces = [part] if count(part) <= limit else _split(part, smaller, count, limit)
        for piece in pieces:
            candidate = current + piece
            if count(candidate) <= limit:
                current = candidate
                continue
            if current:
                chunks.append(current)
            current = piece
    if current:
        chunks.append(current)
    return chunks


# The last resort, for a run of text with no separator at all: the longest prefix that fits, found by halving, so a 100 MB line
# costs a logarithm of counts per chunk and not one per character.
def _by_characters(text: str, count: Callable[[str], int], limit: int) -> list[str]:
    chunks: list[str] = []
    rest = text
    while rest:
        low, high = 1, len(rest)
        while low < high:
            middle = (low + high + 1) // 2
            if count(rest[:middle]) <= limit:
                low = middle
            else:
                high = middle - 1
        chunks.append(rest[:low])
        rest = rest[low:]
    return chunks
