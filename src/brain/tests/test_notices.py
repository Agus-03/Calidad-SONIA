# The fixed answer of decision O2, and the one file of rules/ whose content the customer reads.
#
# What is asserted here and nowhere else: that the file cannot contradict O1c. A text for the triage would send a message to
# somebody the design decided to answer with a person, and there is no other place where that mistake would show.

from pathlib import Path

import pytest
import yaml

from infra import rules as rules_module
from infra.enums import MessageDirection, PipelineStep
from infra.rules import RulesFileError
from pipeline import blocklist, notices


@pytest.fixture(autouse=True)
def _fresh_rules():
    notices.rules.cache_clear()
    yield
    notices.rules.cache_clear()


# Writes the file the module actually reads, so what is under test is the shape of notices.yaml and not a dict built in Python.
def written(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    *,
    inbound: dict[str, str] | None = None,
    outbound: dict[str, str] | None = None,
    version: int = 1,
) -> None:
    payload = {"version": version, "inbound": inbound or {}, "outbound": outbound or {}}
    (tmp_path / "notices.yaml").write_text(yaml.safe_dump(payload, allow_unicode=True), encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    notices.rules.cache_clear()


# The three cuts a turn can take today, each with its text: the blocklist on the way in, and the blocklist and the guardrail over
# an answer that was already generated. Without this, a refused turn leaves the customer talking to nobody (O2).
def test_the_shipped_file_answers_every_cut_a_turn_can_take_today() -> None:
    spec = notices.rules()

    assert spec.version >= 1
    assert spec.text_for(PipelineStep.BLOCKLIST, MessageDirection.INBOUND)
    assert spec.text_for(PipelineStep.BLOCKLIST, MessageDirection.OUTBOUND)
    assert spec.text_for(PipelineStep.GUARDRAIL, MessageDirection.OUTBOUND)


# Decision O1c, which is the reason this file has a shape at all: a question outside the domain is not told anything. The core
# reads the absence as silence, drops the conversation to waiting, and a person answers.
def test_the_triage_has_no_text_because_o1c_answers_it_with_a_person() -> None:
    assert notices.rules().text_for(PipelineStep.TRIAGE, MessageDirection.INBOUND) is None


# And it cannot be given one by editing the file: O1c is a decision of the design and not a knob of the business, so the file that
# contradicts it does not load at all.
def test_a_text_for_the_triage_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound={"triage": "no atendemos eso"})

    with pytest.raises(RulesFileError, match="O1c"):
        notices.rules()


# The inbound guardrail redacts and never cuts, and nothing after the triage cuts yet: a step with no entry says nothing, which is
# the same shape as the triage and is why the case above exists.
@pytest.mark.parametrize(
    ("step", "direction"),
    [
        (PipelineStep.GUARDRAIL, MessageDirection.INBOUND),
        (PipelineStep.GROUNDING, MessageDirection.INBOUND),
        (PipelineStep.GENERATION, MessageDirection.OUTBOUND),
    ],
)
def test_a_step_that_does_not_cut_has_nothing_to_say(step: PipelineStep, direction: MessageDirection) -> None:
    assert notices.rules().text_for(step, direction) is None


# A typo is refused and not read as silence: "blocklst" would otherwise ship as a cut point that answers nothing, which is exactly
# what a correct triage entry looks like, and the customer would find out before we did.
def test_a_key_that_is_not_a_step_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, inbound={"blocklst": "casi"})

    with pytest.raises(RulesFileError):
        notices.rules()


# A leg that is not there is a file half written and not a leg with nothing to say: the two are told apart by the section being
# there, the same reading blocklist.yaml gets.
def test_a_file_with_no_section_for_a_leg_is_refused(tmp_path, monkeypatch) -> None:
    (tmp_path / "notices.yaml").write_text("version: 1\ninbound: {}\n", encoding="utf-8")
    monkeypatch.setattr(rules_module, "RULES_DIR", tmp_path)
    notices.rules.cache_clear()

    with pytest.raises(RulesFileError):
        notices.rules()


def test_an_empty_text_is_refused_while_loading(tmp_path, monkeypatch) -> None:
    written(tmp_path, monkeypatch, outbound={"blocklist": "   "})

    with pytest.raises(RulesFileError):
        notices.rules()


# A notice is sent as it is written: it is not generated, so it never runs the outbound blocklist, and nothing else would catch a
# forbidden word in one of these texts. Normalized the way the step reads a message, because "pasé" and "pase" are one word to it.
def test_no_shipped_notice_carries_a_word_the_aiagent_may_not_send() -> None:
    forbidden = blocklist.rules().terms(MessageDirection.OUTBOUND)

    for notice in notices.rules().notices:
        text = blocklist._normalize(notice.text)
        matched = [term.written for term in forbidden if term.pattern.search(text)]

        assert matched == [], f"the notice of {notice.step} on the {notice.direction} leg says {matched}"
