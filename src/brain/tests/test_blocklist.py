# The blocklist of ADR-0016: the first step that can end a turn, and the only one whose entire decision is a file.
#
# What is asserted here and nowhere else: that a blocked result carries halts. The runner obeys that field and never reads an
# outcome, because the outcome domain is declared per step — so proving the two agree is each step's own obligation and not the
# runner's, which would otherwise have to know all seven.

from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest
import yaml

from infra import rules as rules_module
from infra.enums import MessageDirection, SenderType
from infra.rules import RulesFileError
from pipeline import blocklist
from pipeline.repository.turn import Agent, Message, Turn
from pipeline.step import StepRequest, StepResult


# Raises instead of answering: this is the step that exists to decide without paying for a model, so any call here is the bug.
class FakeClient:
    provider = "double"

    async def complete(self, *, model: str, system=None, messages=(), temperature=None, max_tokens=None):
        raise AssertionError("the blocklist asked a provider for something a list of terms already answers")


def turn() -> Turn:
    return Turn(
        conversation_id=uuid4(),
        message_batch_id=uuid4(),
        turn_number=1,
        agent=Agent(
            id=uuid4(),
            name="Agente demo",
            llm_provider="cohere",
            llm_model="command-a-03-2025",
            temperature=None,
            max_tokens=512,
        ),
        incoming=(
            Message(
                direction=MessageDirection.INBOUND,
                sender_type=SenderType.CONTACT,
                content_text="hola",
                sent_at=datetime.now(UTC),
            ),
        ),
        history=(),
    )


# rules() caches for the whole process, which is what production wants and what would otherwise let the order of these tests
# decide their result: the first one to run would fix the list for all the rest.
@pytest.fixture(autouse=True)
def _fresh_rules():
    blocklist.rules.cache_clear()
    yield
    blocklist.rules.cache_clear()


# Writes the two files the step actually reads, so what is under test is their shape and not a dict built in Python. Both of them
# every time, because neither is optional in production and a test that left one out would exercise a deploy that cannot happen.
def written(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    *,
    inbound: tuple[str, ...] = (),
    outbound: tuple[str, ...] = (),
    profanity: tuple[str, ...] = (),
    version: int = 1,
    profanity_version: int = 1,
) -> None:
    payload = {
        "version": version,
        "inbound": {"terms": list(inbound)},
        "outbound": {"terms": list(outbound)},
    }
    (tmp_path / "blocklist.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    (tmp_path / "profanity.yaml").write_text(
        yaml.safe_dump({"version": profanity_version, "terms": list(profanity)}, allow_unicode=True), encoding="utf-8"
    )
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    blocklist.rules.cache_clear()


async def decide(text: str, direction: MessageDirection = MessageDirection.INBOUND) -> StepResult:
    return await blocklist.run(StepRequest(turn=turn(), client=FakeClient(), direction=direction, text=text))


# Decision D4 written down where it cannot be forgotten: the mechanism ships complete and the policy ships absent, because the
# anteproyecto declares no blocked terms and anything in this file would be ours. Filling it has to be a deliberate act, and this
# test is what turns it into one. It is about blocklist.yaml alone — the words of O6b are in profanity.yaml, and the case below.
def test_the_shipped_list_is_empty_on_purpose() -> None:
    spec = blocklist.rules()

    assert spec.version >= 1
    assert spec.inbound == (), "somebody wrote terms into blocklist.yaml without deciding D4"
    assert spec.outbound == ()


# The other file, and the one exception to D4: O6b says an insult in an answer can never reach a customer because the exposure is
# legal, so this guard is the team's and ships loaded. Empty, it would leave the only thing O6b asks for unguarded.
def test_the_shipped_profanity_file_carries_the_words_of_o6b() -> None:
    spec = blocklist.rules()

    assert spec.profanity_version >= 1
    assert spec.profanity != (), "profanity.yaml lost the words O6b says cannot reach a customer"
    assert {term.file for term in spec.profanity} == {"profanity.yaml"}


# The question anybody loading a short word asks, and the one this step has to answer with yes: it is that word and not a piece of
# another. Over the shipped list and not over an example of ours, so it stays true for whatever gets loaded later: every term
# matches the word it is, and none of them matches glued inside a longer one.
async def test_no_shipped_word_fires_inside_a_longer_word() -> None:
    for term in blocklist.rules().profanity:
        glued = await decide(f"la palabra x{term.written}x no es esa", MessageDirection.OUTBOUND)
        alone = await decide(f"la palabra {term.written} es esa", MessageDirection.OUTBOUND)

        assert glued.outcome == "pass", f"{term.written!r} fired inside a longer word"
        assert alone.outcome == "blocked", f"{term.written!r} does not match the word it is"


# The same thing said on the case that raised it, with the four spellings that have to be told apart: the word inside another word,
# the word next to punctuation, and the two derivatives. The last two are the price of being exact — "culos" is another word, and
# blocking it is a line in profanity.yaml and not a change here.
@pytest.mark.parametrize(
    ("answer", "outcome"),
    [
        ("tengo un músculo dolorido", "pass"),
        ("se rompió el vínculo laboral", "pass"),
        ("el vehículo está en la cochera", "pass"),
        ("no me rompas el culo", "blocked"),
        ("¡CULO!", "blocked"),
        ("ese culo.", "blocked"),
        ("dos culos", "pass"),
        ("un culón", "pass"),
    ],
)
async def test_a_short_word_matches_that_word_and_no_other(tmp_path, monkeypatch, answer: str, outcome: str) -> None:
    written(tmp_path, monkeypatch, profanity=("culo",))

    assert (await decide(answer, MessageDirection.OUTBOUND)).outcome == outcome


# The guard for whoever keeps loading words into that file, which is the whole reason it is a file: a word of the business in it
# does not filter an insult, it gags the AIAgent until somebody deploys again, and every turn pays for an answer nobody reads.
@pytest.mark.parametrize(
    "answer",
    [
        "el departamento de Nueva Córdoba está disponible desde el lunes",
        "te confirmo la seña y te paso el contrato para firmar",
        "el alquiler es de 350.000 pesos por mes más expensas",
        "si el pago se retrasa hay un recargo del 10 por ciento",
        "gracias por escribirnos, ¿querés que coordinemos una visita?",
    ],
)
async def test_no_shipped_word_blocks_an_ordinary_answer_of_the_business(answer: str) -> None:
    result = await decide(answer, MessageDirection.OUTBOUND)

    assert result.outcome == "pass", "a word of profanity.yaml is also a word this business answers with"


# Only on the way out, which is what O6b is about: cutting the customer's own insult would leave them with no answer for having
# sworn, and what the decision asks is that the AIAgent not be the one swearing.
async def test_the_words_of_o6b_are_read_only_on_the_way_out(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, profanity=("insulto",))

    assert (await decide("sos un insulto", MessageDirection.OUTBOUND)).outcome == "blocked"
    assert (await decide("sos un insulto", MessageDirection.INBOUND)).outcome == "pass"


# Two lists feed the outbound run, so a person reading a blocked answer has to be told which file to open.
async def test_a_blocked_answer_names_the_file_the_term_came_from(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, outbound=("negocio",), profanity=("insulto",))

    blocked = await decide("un insulto", MessageDirection.OUTBOUND)
    assert blocked.detail == {"term": "insulto", "file": "profanity.yaml"}
    assert (await decide("un negocio", MessageDirection.OUTBOUND)).detail == {"term": "negocio", "file": "blocklist.yaml"}


# A list somebody keeps loading gets the same word twice, and gets it in two spellings that are one word once folded.
def test_the_same_word_loaded_twice_is_one_term(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, profanity=("Insulto", "insulto", "ínsúltó"))

    assert [term.written for term in blocklist.rules().profanity] == ["Insulto"]


# Missing and not empty: an empty list is a policy somebody chose, and a file that is not there is a deploy that lost it. This step
# would then answer with the one guard O6b says can never be off, and its row would say the list had done its job.
def test_a_profanity_file_that_is_not_there_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    (tmp_path / "blocklist.yaml").write_text(
        "version: 1\ninbound:\n  terms: []\noutbound:\n  terms: []\n", encoding="utf-8"
    )
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    blocklist.rules.cache_clear()

    with pytest.raises(RulesFileError):
        blocklist.rules()


async def test_a_text_no_term_matched_passes_and_comes_out_untouched(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("prohibido",))

    result = await decide("hola, quiero alquilar el departamento de Nueva Córdoba")

    assert result.outcome == "pass"
    assert result.halts is False
    assert result.text == "hola, quiero alquilar el departamento de Nueva Córdoba", "a step that only decides rewrote a text"
    assert result.detail is None
    assert result.score is None


# The cut, and the assertion the runner cannot make for itself: blocked and halts are one decision said twice, and a blocked that
# forgot the flag would let the turn keep going and answer a message it had already refused.
async def test_a_forbidden_term_ends_the_turn(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("prohibido",))

    result = await decide("esto es prohibido")

    assert result.outcome == "blocked"
    assert result.halts is True, "a blocked step did not end the turn, so the pipeline kept walking after it had refused"
    assert result.text == "esto es prohibido", "the blocklist changed a text it was only supposed to refuse"


# Whole word, which is what keeps a four-letter term from blocking half the conversation of a real estate business.
async def test_a_term_matches_a_whole_word_and_not_a_piece_of_one(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("casa",))

    assert (await decide("hay un casamiento el sábado")).outcome == "pass"
    assert (await decide("me interesa la casa de la esquina")).outcome == "blocked"


# Both sides folded, because a customer writes the way they write: a list that has to be kept in every spelling does not block.
@pytest.mark.parametrize("text", ["te dejo la seña", "te dejo la SEÑA", "te dejo la sena", "te dejo la Seña"])
async def test_case_and_accents_are_folded_on_both_sides(tmp_path, monkeypatch, text: str) -> None:
    written(tmp_path, monkeypatch, inbound=("Seña",))

    assert (await decide(text)).outcome == "blocked", "the same word in another spelling went through"


async def test_a_term_can_hold_more_than_one_word(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("por debajo de la mesa",))

    assert (await decide("te lo pago por debajo de la mesa")).outcome == "blocked"
    assert (await decide("la llave está debajo de la mesa")).outcome == "pass"


# Two lists and not one: what the customer may not write is not what the AIAgent may not answer, and ADR-0016 already counts the
# two runs of this step as two rows of the trace.
async def test_each_direction_reads_its_own_list(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("entrada",), outbound=("salida",))

    assert (await decide("una entrada", MessageDirection.INBOUND)).outcome == "blocked"
    assert (await decide("una entrada", MessageDirection.OUTBOUND)).outcome == "pass"
    assert (await decide("una salida", MessageDirection.OUTBOUND)).outcome == "blocked"
    assert (await decide("una salida", MessageDirection.INBOUND)).outcome == "pass"


# The term as the file writes it, so whoever reads a blocked turn can find the line that caused it. And nothing else: detail_json
# is evidence a person reads, and the customer's message has had no redaction at this point in the turn (ADR-0006).
async def test_the_detail_names_the_term_and_never_the_text_it_was_found_in(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("Seña",))

    result = await decide("mi CBU es 0000003100010000000001 y te paso la seña")

    assert result.detail == {"term": "Seña", "file": "blocklist.yaml"}
    assert "0000003100010000000001" not in str(result.detail), "a blocked row carried the datum ADR-0006 keeps out"


async def test_the_first_term_of_the_file_is_the_one_reported(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("uno", "dos"))

    assert (await decide("dos y uno")).detail == {"term": "uno", "file": "blocklist.yaml"}


# varchar in the row, and for this step it is the file: a turn that got cut has to name the list that cut it (regla 9).
async def test_the_version_of_the_file_is_what_the_row_will_carry(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("prohibido",), version=7)

    assert (await decide("hola")).version == "7"
    assert (await decide("prohibido")).version == "7"


# Two files decided the outbound run, so the row names both: a word loaded into one of them without its own version moving would
# leave every earlier row of this step claiming a list nobody can read back.
async def test_the_outbound_row_names_the_version_of_both_files(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, profanity=("insulto",), version=7, profanity_version=3)

    assert (await decide("hola", MessageDirection.OUTBOUND)).version == "7+profanity3"
    assert (await decide("un insulto", MessageDirection.OUTBOUND)).version == "7+profanity3"
    assert (await decide("hola", MessageDirection.INBOUND)).version == "7", "the inbound run never reads profanity.yaml"


# Refused while loading and not while answering: a pattern built out of nothing matches every message, so this one term would
# block the whole business and every row of the trace would say the list had done its job.
def test_a_term_that_is_nothing_once_normalized_is_refused(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound=("́",))

    with pytest.raises(RulesFileError):
        blocklist.rules()


# A direction with no list is a file half written and not an empty policy: the two are told apart by the section being there. The
# second file is written here so what refuses this one is the missing section and not the missing file.
def test_a_file_with_no_list_for_a_direction_is_refused(tmp_path, monkeypatch) -> None:
    (tmp_path / "blocklist.yaml").write_text("version: 1\ninbound:\n  terms: []\n", encoding="utf-8")
    (tmp_path / "profanity.yaml").write_text("version: 1\nterms: []\n", encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    blocklist.rules.cache_clear()

    with pytest.raises(RulesFileError):
        blocklist.rules()
