# The port of regla 11 exercised without a key and without the network: OE2 compares providers, so all four have to answer alike.

import asyncio
from types import SimpleNamespace

import pytest

from adapters.llm import anthropic as anthropic_adapter
from adapters.llm import cohere as cohere_adapter
from adapters.llm import openai as openai_adapter
from adapters.llm.base import (
    DEFAULT_MAX_TOKENS,
    SDK_RETRIES,
    LLMError,
    LLMMessage,
    LLMRefusal,
    LLMTimeout,
    LLMUnavailable,
    calling,
)
from adapters.llm.double import PREFIX, DoubleClient
from adapters.llm.registry import UnavailableClient, clear_clients, resolve
from infra.config import Settings

MESSAGES = (LLMMessage(role="user", text="hola, tienen turno?"),)


def configured(**overrides: object) -> Settings:
    return Settings(
        _env_file=None,
        database_url="postgresql://sonia:sonia@127.0.0.1:5432/sonia",
        brain_internal_token="test-token",
        **overrides,
    )


@pytest.fixture(autouse=True)
def _drop_cached_clients() -> None:
    # Every case starts without the clients another one built, or the credential under test would not be the one used.
    clear_clients()


# --- el contrato de errores ---------------------------------------------------


async def test_calling_reports_a_timeout_as_its_own_error() -> None:
    with pytest.raises(LLMTimeout):
        async with calling("cohere", 0.01):
            await asyncio.sleep(1)


async def test_calling_wraps_what_the_sdk_raises() -> None:
    with pytest.raises(LLMError) as raised:
        async with calling("cohere", 5):
            raise ValueError("503 from upstream")

    assert "cohere: ValueError" in str(raised.value), "the log has to name which provider failed"


# A refusal already is an LLMError and must not be re-wrapped, or the distinction that decides the retry is lost.
async def test_calling_lets_a_refusal_through_untouched() -> None:
    with pytest.raises(LLMRefusal):
        async with calling("cohere", 5):
            raise LLMRefusal("cohere: the model declined to answer")


# --- el registro --------------------------------------------------------------


def test_resolve_returns_the_client_the_row_names() -> None:
    settings = configured(cohere_api_key="k", anthropic_api_key="k", openai_api_key="k")

    assert resolve("cohere", settings).provider == "cohere"
    assert resolve("anthropic", settings).provider == "anthropic"
    assert resolve("openai", settings).provider == "openai"


# One client per connection: building one per turn throws away the SDK's connection pool.
def test_resolve_reuses_the_client_of_the_same_credential() -> None:
    settings = configured(cohere_api_key="k")

    assert resolve("cohere", settings) is resolve("cohere", settings)


@pytest.mark.parametrize(
    ("provider", "expected"),
    [
        ("cohere", "COHERE_API_KEY"),
        ("anthropic", "ANTHROPIC_API_KEY"),
        ("openai", "OPENAI_API_KEY"),
    ],
)
async def test_resolve_answers_with_a_client_that_fails_on_a_missing_key(
    provider: str, expected: str
) -> None:
    client = resolve(provider, configured())

    assert isinstance(client, UnavailableClient)
    with pytest.raises(LLMUnavailable) as raised:
        await client.complete(model="m", system=None, messages=MESSAGES)
    assert expected in str(raised.value), "the failure has to name what is missing"


# A provider nobody wrote an adapter for is the same kind of dead end as a missing key, and fails the same way.
async def test_resolve_answers_with_a_client_that_fails_on_an_unknown_provider() -> None:
    client = resolve("marte", configured(cohere_api_key="k"))

    with pytest.raises(LLMUnavailable) as raised:
        await client.complete(model="m", system=None, messages=MESSAGES)
    assert "unknown provider" in str(raised.value)


# The row is what picks a provider, but a deployment without keys has to be able to answer anyway.
def test_the_double_wins_over_the_row() -> None:
    settings = configured(cohere_api_key="k", llm_use_double=True, environment="test")

    assert resolve("cohere", settings).provider == "double"


# --- el doblado ---------------------------------------------------------------


async def test_the_double_answers_with_the_message_it_received() -> None:
    completion = await DoubleClient().complete(
        model="doble",
        system=None,
        messages=(*MESSAGES, LLMMessage(role="assistant", text="ya voy")),
    )

    assert completion.text == f"{PREFIX} hola, tienen turno?"
    assert completion.prompt_tokens > 0
    assert completion.completion_tokens > 0


async def test_the_double_refuses_a_turn_with_nothing_to_answer() -> None:
    with pytest.raises(LLMUnavailable):
        await DoubleClient().complete(model="doble", system=None, messages=())


# --- los tres proveedores -----------------------------------------------------


# Records what the adapter sent and answers what the case declares, so no test needs the network.
class FakeSDK:
    def __init__(self, response: object) -> None:
        self.response = response
        self.built_with: dict[str, object] = {}
        self.called_with: dict[str, object] = {}

    async def call(self, **kwargs: object) -> object:
        self.called_with = kwargs
        return self.response


def cohere_response(finish_reason: str = "COMPLETE") -> SimpleNamespace:
    return SimpleNamespace(
        finish_reason=finish_reason,
        message=SimpleNamespace(
            content=[
                SimpleNamespace(type="thinking", text="ignorame"),
                SimpleNamespace(type="text", text="Si, tenemos."),
            ]
        ),
        # Floats on purpose: this provider types the counts as numbers and generations.prompt_tokens is an integer.
        usage=SimpleNamespace(tokens=SimpleNamespace(input_tokens=11.0, output_tokens=4.0)),
    )


def anthropic_response(stop_reason: str = "end_turn") -> SimpleNamespace:
    return SimpleNamespace(
        stop_reason=stop_reason,
        content=[
            SimpleNamespace(type="thinking", text="ignorame"),
            SimpleNamespace(type="text", text="Si, tenemos."),
        ],
        usage=SimpleNamespace(input_tokens=11, output_tokens=4),
    )


def openai_response(finish_reason: str = "stop") -> SimpleNamespace:
    return SimpleNamespace(
        choices=[
            SimpleNamespace(
                finish_reason=finish_reason,
                message=SimpleNamespace(content="Si, tenemos."),
            )
        ],
        usage=SimpleNamespace(prompt_tokens=11, completion_tokens=4),
    )


# Hangs the recording coroutine off the attribute path each SDK exposes, so the adapter runs through its real call.
def _factory(sdk: FakeSDK, path: str):
    def build(**kwargs: object) -> FakeSDK:
        sdk.built_with = kwargs
        node: object = sdk
        parts = path.split(".")
        for part in parts[:-1]:
            child = SimpleNamespace()
            setattr(node, part, child)
            node = child
        setattr(node, parts[-1], sdk.call)
        return sdk

    return build


def build_cohere(monkeypatch: pytest.MonkeyPatch, response: object):
    sdk = FakeSDK(response)
    monkeypatch.setattr(cohere_adapter, "AsyncClientV2", _factory(sdk, "chat"))
    return cohere_adapter.CohereClient(api_key="k", timeout=3), sdk


def build_anthropic(monkeypatch: pytest.MonkeyPatch, response: object):
    sdk = FakeSDK(response)
    monkeypatch.setattr(anthropic_adapter, "AsyncAnthropic", _factory(sdk, "messages.create"))
    return anthropic_adapter.AnthropicClient(api_key="k", timeout=3), sdk


def build_openai(monkeypatch: pytest.MonkeyPatch, response: object):
    sdk = FakeSDK(response)
    monkeypatch.setattr(openai_adapter, "AsyncOpenAI", _factory(sdk, "chat.completions.create"))
    return openai_adapter.OpenAIClient(api_key="k", timeout=3), sdk


BUILDERS = {
    "anthropic": (build_anthropic, anthropic_response),
    "cohere": (build_cohere, cohere_response),
    "openai": (build_openai, openai_response),
}
REFUSALS = {"cohere": "ERROR_TOXIC", "anthropic": "refusal", "openai": "content_filter"}


@pytest.mark.parametrize("provider", sorted(BUILDERS))
async def test_the_three_providers_return_the_same_completion(
    monkeypatch: pytest.MonkeyPatch, provider: str
) -> None:
    build, response = BUILDERS[provider]
    client, _ = build(monkeypatch, response())

    completion = await client.complete(model="un-modelo", system="sos SONIA", messages=MESSAGES)

    assert completion.text == "Si, tenemos.", "only the text blocks are the answer"
    assert completion.model == "un-modelo"
    assert (completion.prompt_tokens, completion.completion_tokens) == (11, 4)


@pytest.mark.parametrize("provider", sorted(BUILDERS))
async def test_the_three_providers_report_a_refusal_as_such(
    monkeypatch: pytest.MonkeyPatch, provider: str
) -> None:
    build, response = BUILDERS[provider]
    client, _ = build(monkeypatch, response(REFUSALS[provider]))

    with pytest.raises(LLMRefusal):
        await client.complete(model="un-modelo", system=None, messages=MESSAGES)


# An absent temperature means the provider's default, and null is a value: sending it changes what the model does.
@pytest.mark.parametrize("provider", sorted(BUILDERS))
async def test_the_three_providers_omit_a_temperature_nobody_set(
    monkeypatch: pytest.MonkeyPatch, provider: str
) -> None:
    build, response = BUILDERS[provider]
    client, sdk = build(monkeypatch, response())

    await client.complete(model="un-modelo", system=None, messages=MESSAGES)
    assert "temperature" not in sdk.called_with

    await client.complete(model="un-modelo", system=None, messages=MESSAGES, temperature=0.2)
    assert sdk.called_with["temperature"] == 0.2


# ai_agents.max_tokens is nullable and Anthropic requires the value, so the port has to carry a floor of its own.
@pytest.mark.parametrize("provider", sorted(BUILDERS))
async def test_the_three_providers_fall_back_to_the_ports_token_limit(
    monkeypatch: pytest.MonkeyPatch, provider: str
) -> None:
    build, response = BUILDERS[provider]
    client, sdk = build(monkeypatch, response())

    await client.complete(model="un-modelo", system=None, messages=MESSAGES, max_tokens=None)

    field = "max_completion_tokens" if provider == "openai" else "max_tokens"
    assert sdk.called_with[field] == DEFAULT_MAX_TOKENS


# Anthropic is the only one of the three that carries the system prompt outside the message list.
@pytest.mark.parametrize(
    ("provider", "in_messages"),
    [("cohere", True), ("openai", True), ("anthropic", False)],
)
async def test_each_provider_places_the_system_prompt_where_it_belongs(
    monkeypatch: pytest.MonkeyPatch, provider: str, in_messages: bool
) -> None:
    build, response = BUILDERS[provider]
    client, sdk = build(monkeypatch, response())

    await client.complete(model="un-modelo", system="sos SONIA", messages=MESSAGES)

    roles = [m["role"] for m in sdk.called_with["messages"]]
    assert ("system" in roles) is in_messages
    assert (sdk.called_with.get("system") == "sos SONIA") is not in_messages


@pytest.mark.parametrize("provider", sorted(BUILDERS))
async def test_no_provider_retries_on_its_own(
    monkeypatch: pytest.MonkeyPatch, provider: str
) -> None:
    build, response = BUILDERS[provider]
    _, sdk = build(monkeypatch, response())

    assert sdk.built_with["max_retries"] == SDK_RETRIES
    assert sdk.built_with["timeout"] == 3
