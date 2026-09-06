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
    'Agente fijo del esqueleto de la Fase 1: con los seis componentes de F01, sin RAG y sin clasificadores.',
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

-- The six of F01, with the illustrative content of 03-capa-prompts section 4.1: the real ones come from the cooperante's
-- survey (section 8.6). Position is the administrator's order and the assembler obeys it, it does not impose one (Regla 1).
INSERT INTO prompt_components (id, ai_agent_id, type, position, content)
VALUES
    ('01930000-0000-7000-8000-000000000005', '01930000-0000-7000-8000-000000000002', 'objective', 1,
     'Ayudar a potenciales inquilinos a encontrar el departamento que mejor se ajuste a su necesidad y coordinar el inicio del proceso de alquiler.'),
    ('01930000-0000-7000-8000-000000000006', '01930000-0000-7000-8000-000000000002', 'personality', 2,
     'Asesor inmobiliario cercano y resolutivo. Pregunta zona, presupuesto y ambientes antes de ofrecer opciones.'),
    ('01930000-0000-7000-8000-000000000007', '01930000-0000-7000-8000-000000000002', 'tone', 3,
     'Formal pero cálido. Usa ''usted'' con clientes nuevos; pasa a ''vos'' si el cliente lo hace primero.'),
    ('01930000-0000-7000-8000-000000000008', '01930000-0000-7000-8000-000000000002', 'brand_voice', 4,
     'Representa a una inmobiliaria familiar con trayectoria en Jesús María y Córdoba Capital. Trato personalizado, no de plataforma automatizada.'),
    ('01930000-0000-7000-8000-000000000009', '01930000-0000-7000-8000-000000000002', 'behavior', 5,
     'Ante una propiedad puntual, consultar la base de conocimiento antes de responder. Si no hay dato, decirlo y ofrecer escalar. Nunca inventar precio, dirección ni disponibilidad.'),
    ('01930000-0000-7000-8000-000000000010', '01930000-0000-7000-8000-000000000002', 'rules', 6,
     'No confirmar reservas ni tomar pagos por este canal. Si la conversación se deriva a un asesor, avisarle al cliente antes y resumirle brevemente lo hablado hasta ahí.')
ON CONFLICT (id) DO NOTHING;

-- The content is copied from the rows above and never retyped: with the same text in two tables, typing it twice is how
-- the evidence starts lying (H-20). Only the id and the type are paired by hand, and neither of those can drift in meaning.
INSERT INTO prompt_component_versions (id, prompt_component_id, version_number, content, active)
SELECT v.id, c.id, 1, c.content, c.active
FROM   prompt_components c
JOIN   (VALUES
           ('01930000-0000-7000-8000-000000000011'::uuid, 'objective'),
           ('01930000-0000-7000-8000-000000000012'::uuid, 'personality'),
           ('01930000-0000-7000-8000-000000000013'::uuid, 'tone'),
           ('01930000-0000-7000-8000-000000000014'::uuid, 'brand_voice'),
           ('01930000-0000-7000-8000-000000000015'::uuid, 'behavior'),
           ('01930000-0000-7000-8000-000000000016'::uuid, 'rules')
       ) AS v (id, type) ON v.type = c.type
WHERE  c.ai_agent_id = '01930000-0000-7000-8000-000000000002'
-- changed_by_operator_id and change_note stay NULL because nobody edited anything: this is a seed and not an edition.
ON CONFLICT (prompt_component_id, version_number) DO NOTHING;
