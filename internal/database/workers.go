package database

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/scythe504/kronos/internal/utils"
)

func (s *service) UpsertWorker(ctx context.Context, tx pgx.Tx, workers []Worker) (string, error) {
	if len(workers) == 0 {
		return "", nil
	}

	encKey := utils.GetEncryptionKey()

	valueStrings := make([]string, 0, len(workers))
	valueArgs := make([]any, 0, len(workers)*10)

	for i, worker := range workers {
		var encryptedEnv *string
		if len(worker.EnvVars) > 0 {
			enc, err := utils.EncryptEnv(string(worker.EnvVars), encKey)
			if err != nil {
				return "", fmt.Errorf("failed to encrypt env_vars for worker %s: %w", worker.Slug, err)
			}
			encryptedEnv = &enc
		}

		baseParam := i * 10
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			baseParam+1, baseParam+2, baseParam+3, baseParam+4,
			baseParam+5, baseParam+6, baseParam+7, baseParam+8,
			baseParam+9, baseParam+10,
		))

		valueArgs = append(valueArgs,
			worker.Slug,
			worker.Name,
			worker.Description,
			worker.RepoURL,
			worker.RepoRef,
			encryptedEnv,
			worker.DockerfilePath,
			worker.Entrypoint,
			worker.TaskUnit,
			worker.TaskTimeoutSeconds,
		)
	}

	upsertWorkerQuery := fmt.Sprintf(`INSERT INTO workers (
		slug, name, description, repo_url,
		repo_ref, env_vars, dockerfile_path, entrypoint, task_unit,
		task_timeout_seconds
	) VALUES %s
		ON CONFLICT (slug)
		DO UPDATE
		SET name = EXCLUDED.name, 
		    description = EXCLUDED.description, 
		    repo_url = EXCLUDED.repo_url, 
		    repo_ref = EXCLUDED.repo_ref,
		    env_vars = EXCLUDED.env_vars, 
		    dockerfile_path = EXCLUDED.dockerfile_path,
		    entrypoint = EXCLUDED.entrypoint,
		    task_unit = EXCLUDED.task_unit,
		    task_timeout_seconds = EXCLUDED.task_timeout_seconds,
		    deleted_at = NULL,
		    updated_at = NOW()
		RETURNING slug
	`, strings.Join(valueStrings, ", "))

	var lastSlug string
	if tx != nil {
		rows, err := tx.Query(ctx, upsertWorkerQuery, valueArgs...)
		if err != nil {
			return "", fmt.Errorf("failed creating or updating workers: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&lastSlug); err != nil {
				return "", err
			}
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	} else {
		rows, err := s.pool.Query(ctx, upsertWorkerQuery, valueArgs...)
		if err != nil {
			return "", fmt.Errorf("failed creating or updating workers: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := rows.Scan(&lastSlug); err != nil {
				return "", err
			}
		}
		if err := rows.Err(); err != nil {
			return "", err
		}
	}

	return lastSlug, nil
}

func (s *service) GetWorker(ctx context.Context, slug string) (Worker, error) {
	encKey := utils.GetEncryptionKey()
	query := `
		SELECT slug, name, description, repo_url, repo_ref, env_vars, dockerfile_path, entrypoint, task_unit, task_timeout_seconds, created_at, updated_at
		FROM workers
		WHERE slug = $1 AND deleted_at IS NULL
	`
	var w Worker
	var encryptedEnv []byte
	err := s.pool.QueryRow(ctx, query, slug).Scan(
		&w.Slug, &w.Name, &w.Description, &w.RepoURL, &w.RepoRef,
		&encryptedEnv, &w.DockerfilePath, &w.Entrypoint,
		&w.TaskUnit, &w.TaskTimeoutSeconds, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return w, err
	}
	if len(encryptedEnv) > 0 {
		dec, err := utils.DecryptEnv(string(encryptedEnv), encKey)
		if err == nil {
			w.EnvVars = []byte(dec)
		} else {
			w.EnvVars = encryptedEnv
		}
	}
	return w, nil
}

func (s *service) GetWorkers(ctx context.Context, page int, perPage int) ([]Worker, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	offset := (page - 1) * perPage

	query := `
		SELECT slug, name, description, repo_url, repo_ref, env_vars, dockerfile_path, entrypoint, task_unit, task_timeout_seconds, created_at, updated_at
		FROM workers
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := s.pool.Query(ctx, query, perPage, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	encKey := utils.GetEncryptionKey()
	var workers []Worker
	for rows.Next() {
		var w Worker
		var encryptedEnv []byte
		if err := rows.Scan(
			&w.Slug, &w.Name, &w.Description, &w.RepoURL, &w.RepoRef,
			&encryptedEnv, &w.DockerfilePath, &w.Entrypoint,
			&w.TaskUnit, &w.TaskTimeoutSeconds, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(encryptedEnv) > 0 {
			dec, err := utils.DecryptEnv(string(encryptedEnv), encKey)
			if err != nil {
				dec = string(encryptedEnv)
			}
			if decoded, b64Err := base64.StdEncoding.DecodeString(dec); b64Err == nil && len(decoded) > 0 {
				w.EnvVars = decoded
			} else {
				w.EnvVars = []byte(dec)
			}
		}
		workers = append(workers, w)
	}
	return workers, nil
}

func (s *service) DeleteWorker(ctx context.Context, slug string) (string, error) {
	query := `
		UPDATE workers
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE slug = $1 AND deleted_at IS NULL
		RETURNING slug
	`
	var deletedSlug string
	err := s.pool.QueryRow(ctx, query, slug).Scan(&deletedSlug)
	if err != nil {
		return "", fmt.Errorf("failed to delete worker %s: %w", slug, err)
	}
	return deletedSlug, nil
}
