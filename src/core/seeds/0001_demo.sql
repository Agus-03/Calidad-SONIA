-- Demo data of the Fase 1 skeleton: idempotent, run by the Makefile and never at boot (regla 3), with literal ids shaped as UUID v7 (C04).

-- The single-row invariant is a unique index over an expression, which ON CONFLICT cannot infer.
INSERT INTO accounts (id, name)
SELECT '01930000-0000-7000-8000-000000000001', 'SONIA - demo'
WHERE NOT EXISTS (SELECT 1 FROM accounts);

-- The model lives in the row and not in the brain config, so trying another one is a seed and not a deploy (ADR-0018).
INSERT INTO ai_agents (id, account_id, name, description, default_mode, llm_provider, llm_model, temperature, max_tokens)
VALUES (
    '01930000-0000-7000-8000-000000000002',
    '01930000-0000-7000-8000-000000000001',
    'Agente demo',
    'Agente fijo del esqueleto de la Fase 1: sin componentes de prompt, sin RAG y sin clasificadores.',
    'autonomous',
    'cohere',
    'command-a-03-2025',
    0.3,
    512
)
ON CONFLICT (id) DO NOTHING;

-- Placeholder credentials: there is no WABA yet and the outbound adapter is doubled, so nothing reads the two bytea (ADR-0006).
INSERT INTO inboxes (id, account_id, default_ai_agent_id, phone_number_id, waba_id, display_name,
                     access_token_encrypted, verify_token, app_secret_encrypted, status)
VALUES (
    '01930000-0000-7000-8000-000000000003',
    '01930000-0000-7000-8000-000000000001',
    '01930000-0000-7000-8000-000000000002',
    '000000000000000',
    '000000000000000',
    'Numero demo',
    '\x00'::bytea,
    'sonia-demo-verify-token',
    '\x00'::bytea,
    'active'
)
ON CONFLICT (id) DO NOTHING;

-- The webhook resolves the sender against this row: creating a contact from a message is Fase 4 (F20).
INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel, consent_given_at)
VALUES (
    '01930000-0000-7000-8000-000000000004',
    '01930000-0000-7000-8000-000000000001',
    '5493510000000',
    'Contacto',
    'Demo',
    'client',
    'direct',
    now()
)
ON CONFLICT (id) DO NOTHING;
