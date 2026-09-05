# The generated module has to say exactly what the contract says, or C07 is a rule nobody enforces.

import enum
from pathlib import Path

import yaml

from infra import enums

_CONTRACT = yaml.safe_load(
    (Path(__file__).resolve().parents[3] / "contracts" / "enums.yaml").read_text(encoding="utf-8")
)


def _generated() -> dict[str, type[enum.Enum]]:
    return {
        name: value
        for name, value in vars(enums).items()
        if isinstance(value, type) and issubclass(value, enum.Enum) and value.__module__ == enums.__name__
    }


def test_the_generated_module_declares_every_enumeration_of_the_contract() -> None:
    assert set(_generated()) == set(_CONTRACT["enums"])


# The order is part of the contract: F01 uses it as the default position of the six prompt components.
def test_every_enumeration_carries_the_declared_values_in_order() -> None:
    for name, body in _CONTRACT["enums"].items():
        generated = [member.value for member in _generated()[name]]

        assert generated == [str(value) for value in body["values"]], name


# C07 exists because "resolved", "Resolved" and "resuelto" can otherwise coexist and a metric counts wrong without failing.
def test_a_value_the_contract_does_not_declare_is_not_a_member() -> None:
    assert enums.ConversationStatus("resolved") is enums.ConversationStatus.RESOLVED

    for wrong in ("Resolved", "resuelto", "", "client"):
        assert wrong not in [member.value for member in enums.ConversationStatus]
