package store

import "context"

func (s *Store) FailedProviders(ctx context.Context, taskID string) (map[string]bool, error) {
	rows, err := s.Pool.Query(ctx, "SELECT provider_id FROM task_attempts WHERE task_id=$1 AND status='failed'", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (s *Store) ListTasks(ctx context.Context, userID string, limit, offset int) ([]Task, int, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, ErrConflict
	}
	var total int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE user_id=$1 AND deleted_at IS NULL", userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT "+taskColumns+" FROM tasks WHERE user_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, rows.Err()
}
