# The reader of rules/: every one of those files names the version that decided a turn (regla 9), so a file that cannot say
# which version it is has to fail on the way up and never halfway through answering somebody.

from pathlib import Path

import pytest

from infra.rules import RULES_DIR, RulesFileError, _load, load


def write(tmp_path: Path, body: str, name: str = "sample.yaml") -> Path:
    path = tmp_path / name
    path.write_text(body, encoding="utf-8")
    return path


# Against a file that ships, because a loader that only ever reads what a test wrote proves nothing about the folder it reads.
def test_a_shipped_rules_file_loads_with_its_version() -> None:
    spec = load("intent.yaml")

    assert spec.name == "intent.yaml"
    assert spec.version >= 1
    assert spec.raw["max_intents"] >= 1


def test_the_folder_is_the_package_and_not_the_working_directory() -> None:
    assert RULES_DIR.is_dir()
    assert (RULES_DIR / "generation.yaml").is_file()


# The message names the file, because the fix is editing that file and whoever reads the log has to know which one.
def test_a_file_that_is_not_there_names_itself(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError) as raised:
        _load(tmp_path / "ausente.yaml")

    assert "ausente.yaml" in str(raised.value)


def test_a_file_that_is_not_yaml_is_a_deployment_mistake(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError):
        _load(write(tmp_path, "version: 1\n  torcido: ["))


# A list or a bare string parses as YAML and is not a rules file: catching it here is what keeps the accessors below simple.
def test_a_file_that_is_not_a_mapping_is_refused(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError) as raised:
        _load(write(tmp_path, "- uno\n- dos\n"))

    assert "not a mapping" in str(raised.value)


def test_a_file_with_no_version_is_refused(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError) as raised:
        _load(write(tmp_path, "system: clasificá esto\n"))

    assert "version" in str(raised.value)


# The trap the whole check exists for: in Python a bool is an int, so version: true would load and reach
# pipeline_steps.version as "True", naming a prompt that nobody can ever find again.
def test_a_boolean_is_not_a_version(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError):
        _load(write(tmp_path, "version: true\n"))


# Versions start at 1 and count up, so a zero is a file somebody left half written and not a first version.
def test_a_version_below_one_is_refused(tmp_path: Path) -> None:
    with pytest.raises(RulesFileError):
        _load(write(tmp_path, "version: 0\n"))


def test_a_section_keeps_the_version_of_the_file_it_came_from(tmp_path: Path) -> None:
    spec = _load(write(tmp_path, "version: 3\nsystem:\n  preamble: sos un clasificador\n"))
    section = spec.section("system")

    assert section.version == 3
    assert section.text("preamble") == "sos un clasificador"
    # Named after the two of them, so an error inside a section says where inside the file to look.
    assert "system" in section.name


def test_a_missing_section_is_refused(tmp_path: Path) -> None:
    spec = _load(write(tmp_path, "version: 1\nsystem: no soy un mapping\n"))

    with pytest.raises(RulesFileError) as raised:
        spec.section("system")

    assert "sample.yaml" in str(raised.value)


# Stripped because a prompt written as a YAML block ends in the newline the editor left, not in one anybody meant.
def test_text_comes_back_stripped(tmp_path: Path) -> None:
    spec = _load(write(tmp_path, "version: 1\nsystem: |-\n  sos un clasificador\n"))

    assert spec.text("system") == "sos un clasificador"


# A blank prompt is worse than a missing one: it reaches the provider and gets an answer that nobody can explain.
@pytest.mark.parametrize("body", ["version: 1\nsystem: '   '\n", "version: 1\nsystem: 7\n", "version: 1\n"])
def test_text_that_is_not_text_is_refused(tmp_path: Path, body: str) -> None:
    spec = _load(write(tmp_path, body))

    with pytest.raises(RulesFileError):
        spec.text("system")


# Stripped one by one for the same reason a single text is, and an empty list admitted on purpose: a mechanism that ships with
# no policy in it yet is a decision somebody took, and a file has no way of saying which of the two it is (blocklist.yaml v1).
def test_a_list_of_texts_comes_back_stripped_and_may_be_empty(tmp_path: Path) -> None:
    spec = _load(write(tmp_path, "version: 1\nterms:\n  - '  uno  '\n  - dos\nempty: []\n"))

    assert spec.strings("terms") == ("uno", "dos")
    assert spec.strings("empty") == ()


# A key that is not a list, one that is not there, and an entry that is not text: the three ways a list of terms can be a file
# somebody left half written, which has to fail on the way up and not on the turn that needed it.
@pytest.mark.parametrize(
    "body",
    [
        "version: 1\nterms: uno\n",
        "version: 1\n",
        "version: 1\nterms:\n  - '  '\n",
        "version: 1\nterms:\n  - 7\n",
    ],
)
def test_a_list_that_is_not_one_of_texts_is_refused(tmp_path: Path, body: str) -> None:
    spec = _load(write(tmp_path, body))

    with pytest.raises(RulesFileError) as raised:
        spec.strings("terms")

    assert "terms" in str(raised.value)


def test_a_count_is_an_integer_at_or_above_its_minimum(tmp_path: Path) -> None:
    spec = _load(write(tmp_path, "version: 1\nmax_intents: 5\n"))

    assert spec.integer("max_intents", minimum=1) == 5


@pytest.mark.parametrize("body", ["version: 1\nmax_intents: 0\n", "version: 1\nmax_intents: true\n", "version: 1\nmax_intents: dos\n"])
def test_a_count_that_is_not_one_is_refused(tmp_path: Path, body: str) -> None:
    spec = _load(write(tmp_path, body))

    with pytest.raises(RulesFileError) as raised:
        spec.integer("max_intents", minimum=1)

    assert "max_intents" in str(raised.value)
