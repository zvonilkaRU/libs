-- +goose Up
CREATE TABLE things (id UUID PRIMARY KEY, name TEXT NOT NULL);

-- +goose Down
DROP TABLE things;
