CREATE TABLE inbox_operators (
    id          uuid        PRIMARY KEY,
    inbox_id    uuid        NOT NULL REFERENCES inboxes (id),
    operator_id uuid        NOT NULL REFERENCES operators (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inbox_operators_inbox_id_operator_id UNIQUE (inbox_id, operator_id)
);

CREATE INDEX idx_inbox_operators_inbox_id ON inbox_operators (inbox_id);
