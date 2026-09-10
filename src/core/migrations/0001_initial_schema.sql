-- The whole model of docs/design/DER: 37 tables, ordered so every foreign key points at something that already exists.

-- Bloque 5 - the container the six root entities hang from (ADR-0012).
CREATE TABLE accounts (
    id         uuid        PRIMARY KEY,
    name       varchar     NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

-- The single-row invariant in DDL and not only in prose: a second row would read as the multi-tenancy that 7.3 excludes.
CREATE UNIQUE INDEX accounts_single_row ON accounts ((true));

CREATE TABLE operators (
    id            uuid        PRIMARY KEY,
    account_id    uuid        NOT NULL REFERENCES accounts (id),
    name          varchar     NOT NULL,
    lastname      varchar     NOT NULL,
    email         varchar     NOT NULL,
    password_hash varchar     NOT NULL,
    role          varchar     NOT NULL CHECK (role IN ('operator', 'supervisor', 'admin')),
    active        boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    deleted_at    timestamptz
);

-- Unique among the not-deleted (C09): taking an operator down frees their address again.
CREATE UNIQUE INDEX operators_email_unique ON operators (email) WHERE deleted_at IS NULL;

-- Bloque 2 - the AI agent is AIAgent and never Agent (C01).
CREATE TABLE ai_agents (
    id                 uuid        PRIMARY KEY,
    account_id         uuid        NOT NULL REFERENCES accounts (id),
    name               varchar     NOT NULL,
    description        text,
    default_mode       varchar     NOT NULL CHECK (default_mode IN ('autonomous', 'suggestion', 'off')),
    extraction_enabled boolean     NOT NULL DEFAULT false,
    llm_provider       varchar     NOT NULL,
    llm_model          varchar     NOT NULL,
    temperature        numeric,
    max_tokens         integer,
    active             boolean     NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz
);

-- Bloque 6 - one inbox per WhatsApp number; several numbers are still the single channel of 7.3 (ADR-0012).
CREATE TABLE inboxes (
    id                     uuid        PRIMARY KEY,
    account_id             uuid        NOT NULL REFERENCES accounts (id),
    default_ai_agent_id    uuid        REFERENCES ai_agents (id),
    phone_number_id        varchar     NOT NULL,
    waba_id                varchar     NOT NULL,
    display_name           varchar     NOT NULL,
    access_token_encrypted bytea       NOT NULL,
    verify_token           varchar     NOT NULL,
    app_secret_encrypted   bytea       NOT NULL,
    status                 varchar     NOT NULL CHECK (status IN ('active', 'inactive', 'error')),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    deleted_at             timestamptz
);

-- Bloque 1 - the four encrypted columns are bytea because ADR-0006 keeps the ciphertext and not the reading.
CREATE TABLE contacts (
    id                 uuid        PRIMARY KEY,
    account_id         uuid        NOT NULL REFERENCES accounts (id),
    phone_whatsapp     varchar     NOT NULL,
    name               varchar     NOT NULL,
    lastname           varchar     NOT NULL,
    email              varchar,
    type               varchar     NOT NULL CHECK (type IN ('client', 'provider', 'prospect')),
    notes              text,
    source_channel     varchar     NOT NULL CHECK (source_channel IN ('meta_ads', 'qr', 'referral', 'direct')),
    dni_encrypted      bytea,
    cuit_encrypted     bytea,
    cuil_encrypted     bytea,
    cbu_encrypted      bytea,
    consent_given_at   timestamptz NOT NULL,
    consent_revoked_at timestamptz,
    opt_out            boolean     NOT NULL DEFAULT false,
    blocked            boolean     NOT NULL DEFAULT false,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz
);

CREATE UNIQUE INDEX contacts_phone_whatsapp_unique ON contacts (phone_whatsapp) WHERE deleted_at IS NULL;

CREATE TABLE conversations (
    id                      uuid        PRIMARY KEY,
    contact_id              uuid        NOT NULL REFERENCES contacts (id),
    inbox_id                uuid        NOT NULL REFERENCES inboxes (id),
    ai_agent_id             uuid        REFERENCES ai_agents (id),
    operator_id             uuid        REFERENCES operators (id),
    status                  varchar     NOT NULL CHECK (status IN ('active', 'waiting', 'resolved')),
    mode                    varchar     NOT NULL CHECK (mode IN ('autonomous', 'suggestion', 'off')),
    starred                 boolean     NOT NULL DEFAULT false,
    quality_score           smallint,
    scored_by_operator_id   uuid        REFERENCES operators (id),
    scored_at               timestamptz,
    first_response_at       timestamptz,
    last_activity_at        timestamptz NOT NULL DEFAULT now(),
    resolved_at             timestamptz,
    resolved_by_operator_id uuid        REFERENCES operators (id),
    unread_count            integer     NOT NULL DEFAULT 0,
    last_read_at            timestamptz,
    last_inbound_at         timestamptz NOT NULL,
    window_expires_at       timestamptz NOT NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    deleted_at              timestamptz
);

-- ADR-0002: an inbound message on a resolved conversation opens a new one, so at most one is unresolved at a time.
CREATE UNIQUE INDEX conversations_one_open_per_contact ON conversations (contact_id) WHERE status <> 'resolved' AND deleted_at IS NULL;

-- Bloque 3 - the turn lives here (ADR-0022); immutable, so opened_at plays the part of created_at.
CREATE TABLE message_batches (
    id              uuid        PRIMARY KEY,
    conversation_id uuid        NOT NULL REFERENCES conversations (id),
    messages_count  smallint    NOT NULL DEFAULT 0,
    turn_number     integer     NOT NULL,
    timer_ms_used   integer     NOT NULL,
    closed_reason   varchar     CHECK (closed_reason IN ('silence', 'max_messages', 'max_duration')),
    error_message   text,
    opened_at       timestamptz NOT NULL DEFAULT now(),
    closed_at       timestamptz,
    processed_at    timestamptz
);

-- ADR-0022 makes this the source of truth of the turn number, and three tables copy it from here.
CREATE UNIQUE INDEX message_batches_turn_unique ON message_batches (conversation_id, turn_number);

-- No deleted_at of any kind: messages are the record F34 to F36 and OE7 are computed on (C09).
CREATE TABLE messages (
    id                 uuid        PRIMARY KEY,
    conversation_id    uuid        NOT NULL REFERENCES conversations (id),
    message_batch_id   uuid        REFERENCES message_batches (id),
    wamid              varchar     UNIQUE,
    context_wamid      varchar,
    direction          varchar     NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    sender_type        varchar     NOT NULL CHECK (sender_type IN ('contact', 'ai_agent', 'operator', 'system')),
    sender_operator_id uuid        REFERENCES operators (id),
    sender_ai_agent_id uuid        REFERENCES ai_agents (id),
    content_text       text        NOT NULL,
    content_type       varchar     NOT NULL CHECK (content_type IN ('text', 'audio', 'image')),
    transcription      text,
    media_id           varchar,
    media_url          text,
    media_mime_type    varchar,
    media_size_bytes   bigint,
    media_duration_ms  integer,
    status             varchar     CHECK (status IN ('sent', 'delivered', 'read', 'failed')),
    error_code         varchar,
    sent_at            timestamptz NOT NULL,
    delivered_at       timestamptz,
    read_at            timestamptz,
    hidden_at          timestamptz,
    redacted_at        timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_sender_matches_type CHECK (
        (sender_type = 'operator' AND sender_operator_id IS NOT NULL AND sender_ai_agent_id IS NULL)
        OR (sender_type = 'ai_agent' AND sender_ai_agent_id IS NOT NULL AND sender_operator_id IS NULL)
        OR (sender_type IN ('contact', 'system') AND sender_operator_id IS NULL AND sender_ai_agent_id IS NULL)
    )
);

-- Bloque 2 - the six components of F01; the admin edits them and they are not in rules/ (regla 9).
CREATE TABLE prompt_components (
    id          uuid        PRIMARY KEY,
    ai_agent_id uuid        NOT NULL REFERENCES ai_agents (id),
    type        varchar     NOT NULL CHECK (type IN ('objective', 'personality', 'behavior', 'tone', 'brand_voice', 'rules')),
    content     text        NOT NULL,
    position    smallint    NOT NULL,
    active      boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

-- F01 declares one component of each type per agent; active is ablation and deleted_at is a definitive removal.
CREATE UNIQUE INDEX prompt_components_one_active_type ON prompt_components (ai_agent_id, type) WHERE active AND deleted_at IS NULL;

-- H-20: active is versioned too, or OE1 compares a set against itself and can only ever return 100 percent.
CREATE TABLE prompt_component_versions (
    id                     uuid        PRIMARY KEY,
    prompt_component_id    uuid        NOT NULL REFERENCES prompt_components (id),
    version_number         integer     NOT NULL,
    content                text        NOT NULL,
    active                 boolean     NOT NULL,
    changed_by_operator_id uuid        REFERENCES operators (id),
    change_note            text,
    created_at             timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX prompt_component_versions_number_unique ON prompt_component_versions (prompt_component_id, version_number);

-- F12 - one row per signal, and the threshold here is mutable: what ruled an evaluation is frozen in signal_evaluations.
CREATE TABLE transfer_policies (
    id          uuid        PRIMARY KEY,
    ai_agent_id uuid        NOT NULL REFERENCES ai_agents (id),
    signal_type varchar     NOT NULL CHECK (signal_type IN ('no_resolution', 'frustration', 'out_of_domain', 'explicit_request')),
    enabled     boolean     NOT NULL DEFAULT true,
    threshold   numeric     NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE TABLE detected_intents (
    id               uuid        PRIMARY KEY,
    message_batch_id uuid        NOT NULL REFERENCES message_batches (id),
    intent_label     varchar     NOT NULL,
    confidence       numeric     NOT NULL,
    position         smallint    NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- ADR-0016 - outcome is varchar and not an enumeration because its domain depends on step, which C07 does not allow.
CREATE TABLE pipeline_steps (
    id               uuid        PRIMARY KEY,
    message_batch_id uuid        NOT NULL REFERENCES message_batches (id),
    step             varchar     NOT NULL CHECK (step IN ('blocklist', 'guardrail', 'triage', 'retrieval', 'reranking', 'grounding', 'generation')),
    direction        varchar     NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    position         smallint    NOT NULL,
    outcome          varchar     NOT NULL,
    score            numeric,
    version          varchar     NOT NULL,
    latency_ms       integer,
    detail_json      jsonb,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- H-02 - the current state, one row per conversation; the history is in conversation_context_versions.
CREATE TABLE conversation_contexts (
    id                  uuid        PRIMARY KEY,
    conversation_id     uuid        NOT NULL UNIQUE REFERENCES conversations (id),
    turn_number         integer     NOT NULL,
    summary             text,
    primary_intent      varchar,
    objections_json     jsonb       NOT NULL DEFAULT '[]'::jsonb,
    urgency_level       smallint,
    budget_signal       varchar,
    extracted_data_json jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversation_context_versions (
    id                 uuid        PRIMARY KEY,
    conversation_id    uuid        NOT NULL REFERENCES conversations (id),
    turn_number        integer     NOT NULL,
    content            jsonb       NOT NULL,
    rewritten_by_model varchar     NOT NULL,
    latency_ms         integer     NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);

-- H-21 - ai_agent_id is the agent that produced this generation, frozen; conversations.ai_agent_id is the current one.
CREATE TABLE generations (
    id                              uuid          PRIMARY KEY,
    conversation_id                 uuid          NOT NULL REFERENCES conversations (id),
    ai_agent_id                     uuid          NOT NULL REFERENCES ai_agents (id),
    message_id                      uuid          REFERENCES messages (id),
    message_batch_id                uuid          REFERENCES message_batches (id),
    conversation_context_version_id uuid          REFERENCES conversation_context_versions (id),
    llm_provider                    varchar       NOT NULL,
    llm_model                       varchar       NOT NULL,
    temperature                     numeric,
    prompt_rendered                 text          NOT NULL,
    completion_text                 text          NOT NULL,
    prompt_tokens                   integer       NOT NULL,
    completion_tokens               integer       NOT NULL,
    latency_ms                      integer       NOT NULL,
    cost                            numeric(14,6) NOT NULL,
    currency                        char(3)       NOT NULL DEFAULT 'USD',
    reasoning_json                  jsonb,
    status                          varchar       NOT NULL CHECK (status IN ('auto_sent', 'pending_review', 'accepted', 'edited', 'rejected', 'blocked')),
    reviewed_by_operator_id         uuid          REFERENCES operators (id),
    reviewed_at                     timestamptz,
    created_at                      timestamptz   NOT NULL DEFAULT now()
);

CREATE TABLE generation_components (
    id                          uuid        PRIMARY KEY,
    generation_id               uuid        NOT NULL REFERENCES generations (id),
    prompt_component_version_id uuid        NOT NULL REFERENCES prompt_component_versions (id),
    position                    smallint    NOT NULL,
    created_at                  timestamptz NOT NULL DEFAULT now()
);

-- Bloque 4 - collection_name points into Qdrant and is deliberately not a foreign key (ADR-0010).
CREATE TABLE knowledge_bases (
    id               uuid        PRIMARY KEY,
    ai_agent_id      uuid        NOT NULL REFERENCES ai_agents (id),
    collection_name  varchar     NOT NULL,
    size_bytes_total bigint      NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);

-- object_key and not bytea: a 100 MB file in a row inflates every backup and the WAL (ADR-0019).
CREATE TABLE documents (
    id                     uuid        PRIMARY KEY,
    knowledge_base_id      uuid        NOT NULL REFERENCES knowledge_bases (id),
    filename               varchar     NOT NULL,
    mime_type              varchar     NOT NULL,
    size_bytes             bigint      NOT NULL,
    checksum               varchar     NOT NULL,
    object_key             varchar     NOT NULL,
    status                 varchar     NOT NULL CHECK (status IN ('pending', 'processing', 'indexed', 'failed')),
    locked_at              timestamptz,
    error_message          text,
    uploaded_by_operator_id uuid       NOT NULL REFERENCES operators (id),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    deleted_at             timestamptz
);

-- The row survives and the vector disappears from Qdrant: retrieval_logs reference it as the evidence of OE6.
CREATE TABLE document_chunks (
    id          uuid        PRIMARY KEY,
    document_id uuid        NOT NULL REFERENCES documents (id),
    chunk_index integer     NOT NULL,
    content     text        NOT NULL,
    token_count integer     NOT NULL,
    vector_id   varchar,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz
);

CREATE TABLE retrieval_logs (
    id                uuid        PRIMARY KEY,
    generation_id     uuid        NOT NULL REFERENCES generations (id),
    document_chunk_id uuid        NOT NULL REFERENCES document_chunks (id),
    similarity_score  numeric     NOT NULL,
    rank              smallint    NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- H-15 - F26 asks for the full reassignment history, which conversations.operator_id alone overwrites.
CREATE TABLE conversation_assignments (
    id                      uuid        PRIMARY KEY,
    conversation_id         uuid        NOT NULL REFERENCES conversations (id),
    from_operator_id        uuid        REFERENCES operators (id),
    to_operator_id          uuid        NOT NULL REFERENCES operators (id),
    assigned_by_operator_id uuid        REFERENCES operators (id),
    reason                  text,
    created_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE internal_notes (
    id              uuid        PRIMARY KEY,
    conversation_id uuid        NOT NULL REFERENCES conversations (id),
    operator_id     uuid        NOT NULL REFERENCES operators (id),
    content         text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);

CREATE TABLE labels (
    id                     uuid        PRIMARY KEY,
    account_id             uuid        NOT NULL REFERENCES accounts (id),
    name                   varchar     NOT NULL,
    color                  varchar     NOT NULL,
    created_by_operator_id uuid        NOT NULL REFERENCES operators (id),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    deleted_at             timestamptz
);

-- Folds case because "Ventas" and "ventas" are one label to whoever tags, and is scoped by account because the catalogue belongs to one (F19).
CREATE UNIQUE INDEX labels_name_unique ON labels (account_id, lower(name)) WHERE deleted_at IS NULL;

-- Physical delete, a declared exception to C09: unlabelling is routine and no metric counts historical labellings.
CREATE TABLE contact_labels (
    id                      uuid        PRIMARY KEY,
    contact_id              uuid        NOT NULL REFERENCES contacts (id),
    label_id                uuid        NOT NULL REFERENCES labels (id),
    assigned_by_operator_id uuid        REFERENCES operators (id),
    created_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT contact_labels_unique UNIQUE (contact_id, label_id)
);

CREATE TABLE conversation_labels (
    id                      uuid        PRIMARY KEY,
    conversation_id         uuid        NOT NULL REFERENCES conversations (id),
    label_id                uuid        NOT NULL REFERENCES labels (id),
    assigned_by_operator_id uuid        REFERENCES operators (id),
    created_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conversation_labels_unique UNIQUE (conversation_id, label_id)
);

-- ADR-0002 - the contact is mandatory and the conversation is not: a reminder outlives the conversation that raised it.
CREATE TABLE reminders (
    id              uuid        PRIMARY KEY,
    contact_id      uuid        NOT NULL REFERENCES contacts (id),
    conversation_id uuid        REFERENCES conversations (id),
    operator_id     uuid        NOT NULL REFERENCES operators (id),
    description     text        NOT NULL,
    due_at          timestamptz NOT NULL,
    notified        boolean     NOT NULL DEFAULT false,
    notified_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);

-- C08 - numeric and never floating point, and the currency travels with the amount or the sum is meaningless.
CREATE TABLE sales (
    id              uuid          PRIMARY KEY,
    conversation_id uuid          NOT NULL REFERENCES conversations (id),
    operator_id     uuid          NOT NULL REFERENCES operators (id),
    ai_agent_id     uuid          REFERENCES ai_agents (id),
    amount          numeric(14,2) NOT NULL,
    currency        char(3)       NOT NULL DEFAULT 'ARS',
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);

CREATE TABLE quick_replies (
    id                     uuid        PRIMARY KEY,
    account_id             uuid        NOT NULL REFERENCES accounts (id),
    title                  varchar     NOT NULL,
    content                text        NOT NULL,
    created_by_operator_id uuid        NOT NULL REFERENCES operators (id),
    active                 boolean     NOT NULL DEFAULT true,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    deleted_at             timestamptz
);

-- F20 - the event log; status and retries mutate while reprocessing and never after, so there is no updated_at.
CREATE TABLE webhook_events (
    id              uuid        PRIMARY KEY,
    inbox_id        uuid        NOT NULL REFERENCES inboxes (id),
    event_type      varchar     NOT NULL CHECK (event_type IN ('message', 'status', 'unknown')),
    wamid           varchar,
    payload_json    jsonb       NOT NULL,
    signature_valid boolean     NOT NULL,
    status          varchar     NOT NULL CHECK (status IN ('received', 'processed', 'failed', 'discarded')),
    retries         smallint    NOT NULL DEFAULT 0,
    error_message   text,
    received_at     timestamptz NOT NULL DEFAULT now(),
    processed_at    timestamptz
);

-- F08 and F37 - deleted_at because a template can disappear on Meta's side without losing the local history.
CREATE TABLE meta_templates (
    id               uuid        PRIMARY KEY,
    inbox_id         uuid        NOT NULL REFERENCES inboxes (id),
    meta_template_id varchar     NOT NULL,
    name             varchar     NOT NULL,
    language         varchar     NOT NULL,
    category         varchar     NOT NULL CHECK (category IN ('marketing', 'utility', 'authentication')),
    status           varchar     NOT NULL CHECK (status IN ('approved', 'pending', 'rejected')),
    body             text        NOT NULL,
    rejection_reason text,
    llm_suggestion   text,
    synced_at        timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);

-- Bloque 7 - H-18: one row per signal evaluated in every processed turn, whether it fired or not, or OE4 is not measurable.
CREATE TABLE signal_evaluations (
    id                uuid        PRIMARY KEY,
    message_batch_id  uuid        NOT NULL REFERENCES message_batches (id),
    signal_type       varchar     NOT NULL CHECK (signal_type IN ('no_resolution', 'frustration', 'out_of_domain', 'explicit_request')),
    score             numeric     NOT NULL,
    threshold_used    numeric     NOT NULL,
    classifier_version varchar    NOT NULL,
    fired             boolean     NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- Reclassifying with a new version writes new rows instead of duplicating, which is what allows the threshold sweep.
CREATE UNIQUE INDEX signal_evaluations_one_per_version ON signal_evaluations (message_batch_id, signal_type, classifier_version);

-- The event points at the evidence and does not copy it: F36 reaches the signal type by join.
CREATE TABLE transfer_events (
    id                   uuid        PRIMARY KEY,
    conversation_id      uuid        NOT NULL REFERENCES conversations (id),
    signal_evaluation_id uuid        NOT NULL REFERENCES signal_evaluations (id),
    turn_number          integer     NOT NULL,
    to_operator_id       uuid        REFERENCES operators (id),
    created_at           timestamptz NOT NULL DEFAULT now()
);

-- Ground truth of OE3 and OE4; gold_value is varchar because its domain depends on annotation_type (ADR-0014).
CREATE TABLE conversation_annotations (
    id                       uuid        PRIMARY KEY,
    conversation_id          uuid        NOT NULL REFERENCES conversations (id),
    annotation_type          varchar     NOT NULL CHECK (annotation_type IN ('transfer', 'frustration', 'intent')),
    gold_value               varchar     NOT NULL,
    annotated_by_operator_id uuid        NOT NULL REFERENCES operators (id),
    notes                    text,
    created_at               timestamptz NOT NULL DEFAULT now()
);

-- A judge metric without a versioned rubric is not a metric.
CREATE TABLE tone_evaluations (
    id              uuid        PRIMARY KEY,
    conversation_id uuid        NOT NULL REFERENCES conversations (id),
    generation_id   uuid        REFERENCES generations (id),
    tone_score      numeric     NOT NULL,
    rubric_version  varchar     NOT NULL,
    judge_model     varchar     NOT NULL,
    rationale       text,
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Bloque 8 - encrypted by default: what the LLM extracts can be personal data (Ley 25.326).
CREATE TABLE contact_attributes (
    id                uuid        PRIMARY KEY,
    contact_id        uuid        NOT NULL REFERENCES contacts (id),
    key               varchar     NOT NULL,
    value_encrypted   bytea       NOT NULL,
    source            varchar     NOT NULL CHECK (source IN ('manual', 'llm')),
    confidence        numeric,
    source_message_id uuid        REFERENCES messages (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

-- ADR-0007 - scoped to AI agent configuration changes; reassignments already live in conversation_assignments.
CREATE TABLE audit_logs (
    id           uuid        PRIMARY KEY,
    operator_id  uuid        NOT NULL REFERENCES operators (id),
    action       varchar     NOT NULL,
    entity_type  varchar     NOT NULL,
    entity_id    uuid        NOT NULL,
    changes_json jsonb       NOT NULL,
    ip_address   inet,
    created_at   timestamptz NOT NULL DEFAULT now()
);
