package repository

import (
	"context"
	"time"

	"github.com/christianselig/apollo-backend/internal/domain"
)

type postgresWatcherRepository struct {
	conn Connection
}

func NewPostgresWatcher(conn Connection) domain.WatcherRepository {
	return &postgresWatcherRepository{conn: conn}
}

func (p *postgresWatcherRepository) fetch(ctx context.Context, query string, args ...interface{}) ([]domain.Watcher, error) {
	rows, err := p.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var watchers []domain.Watcher
	for rows.Next() {
		var watcher domain.Watcher
		var subredditLabel, userLabel string

		if err := rows.Scan(
			&watcher.ID,
			&watcher.CreatedAt,
			&watcher.LastNotifiedAt,
			&watcher.Label,
			&watcher.DeviceID,
			&watcher.AccountID,
			&watcher.Type,
			&watcher.WatcheeID,
			&watcher.Author,
			&watcher.Subreddit,
			&watcher.Upvotes,
			&watcher.Keyword,
			&watcher.Flair,
			&watcher.Domain,
			&watcher.Hits,
			&watcher.Device.ID,
			&watcher.Device.APNSToken,
			&watcher.Device.Sandbox,
			&watcher.Account.ID,
			&watcher.Account.AccountID,
			&watcher.Account.AccessToken,
			&watcher.Account.RefreshToken,
			&subredditLabel,
			&userLabel,
		); err != nil {
			return nil, err
		}

		switch watcher.Type {
		case domain.SubredditWatcher, domain.TrendingWatcher:
			watcher.WatcheeLabel = subredditLabel
		case domain.UserWatcher:
			watcher.WatcheeLabel = userLabel
		}

		watchers = append(watchers, watcher)
	}
	return watchers, nil
}

func (p *postgresWatcherRepository) GetByID(ctx context.Context, id int64) (domain.Watcher, error) {
	query := `
		SELECT
			watchers.id,
			watchers.created_at,
			watchers.last_notified_at,
			watchers.label,
			watchers.device_id,
			watchers.account_id,
			watchers.type,
			watchers.watchee_id,
			watchers.author,
			watchers.subreddit,
			watchers.upvotes,
			watchers.keyword,
			watchers.flair,
			watchers.domain,
			watchers.hits,
			devices.id,
			devices.apns_token,
			devices.sandbox,
			accounts.id,
			accounts.reddit_account_id,
			accounts.access_token,
			accounts.refresh_token,
			COALESCE(subreddits.name, '') AS subreddit_label,
			COALESCE(users.name, '') AS user_label
		FROM watchers
		INNER JOIN devices ON watchers.device_id = devices.id
		INNER JOIN accounts ON watchers.account_id = accounts.id
		LEFT JOIN subreddits ON watchers.type IN(0,2) AND watchers.watchee_id = subreddits.id
		LEFT JOIN users ON watchers.type = 1 AND watchers.watchee_id = users.id
		WHERE watchers.id = $1`

	watchers, err := p.fetch(ctx, query, id)

	if err != nil {
		return domain.Watcher{}, err
	}
	if len(watchers) == 0 {
		return domain.Watcher{}, domain.ErrNotFound
	}
	return watchers[0], nil
}

func (p *postgresWatcherRepository) GetByTypeAndWatcheeID(ctx context.Context, typ domain.WatcherType, id int64) ([]domain.Watcher, error) {
	query := `
		SELECT
			watchers.id,
			watchers.created_at,
			watchers.last_notified_at,
			watchers.label,
			watchers.device_id,
			watchers.account_id,
			watchers.type,
			watchers.watchee_id,
			watchers.author,
			watchers.subreddit,
			watchers.upvotes,
			watchers.keyword,
			watchers.flair,
			watchers.domain,
			watchers.hits,
			devices.id,
			devices.apns_token,
			devices.sandbox,
			accounts.id,
			accounts.reddit_account_id,
			accounts.access_token,
			accounts.refresh_token,
			COALESCE(subreddits.name, '') AS subreddit_label,
			COALESCE(users.name, '') AS user_label
		FROM watchers
		INNER JOIN devices ON watchers.device_id = devices.id
		INNER JOIN accounts ON watchers.account_id = accounts.id
		INNER JOIN devices_accounts ON devices.id = devices_accounts.device_id AND accounts.id = devices_accounts.account_id
		LEFT JOIN subreddits ON watchers.type IN(0,2) AND watchers.watchee_id = subreddits.id
		LEFT JOIN users ON watchers.type = 1 AND watchers.watchee_id = users.id
		WHERE watchers.type = $1 AND
		watchers.watchee_id = $2 AND
		devices_accounts.watcher_notifiable = TRUE AND
		devices_accounts.global_mute = FALSE`

	return p.fetch(ctx, query, int64(typ), id)
}

func (p *postgresWatcherRepository) GetByTrendingSubredditID(ctx context.Context, id int64) ([]domain.Watcher, error) {
	return p.GetByTypeAndWatcheeID(ctx, domain.TrendingWatcher, id)
}

func (p *postgresWatcherRepository) GetBySubredditID(ctx context.Context, id int64) ([]domain.Watcher, error) {
	return p.GetByTypeAndWatcheeID(ctx, domain.SubredditWatcher, id)
}

func (p *postgresWatcherRepository) GetByUserID(ctx context.Context, id int64) ([]domain.Watcher, error) {
	return p.GetByTypeAndWatcheeID(ctx, domain.UserWatcher, id)
}

func (p *postgresWatcherRepository) GetByDeviceAPNSTokenAndAccountRedditID(ctx context.Context, apns string, rid string) ([]domain.Watcher, error) {
	query := `
		SELECT
			watchers.id,
			watchers.created_at,
			watchers.last_notified_at,
			watchers.label,
			watchers.device_id,
			watchers.account_id,
			watchers.type,
			watchers.watchee_id,
			watchers.author,
			watchers.subreddit,
			watchers.upvotes,
			watchers.keyword,
			watchers.flair,
			watchers.domain,
			watchers.hits,
			devices.id,
			devices.apns_token,
			devices.sandbox,
			accounts.id,
			accounts.reddit_account_id,
			accounts.access_token,
			accounts.refresh_token,
			COALESCE(subreddits.name, '') AS subreddit_label,
			COALESCE(users.name, '') AS user_label
		FROM watchers
		INNER JOIN accounts ON watchers.account_id = accounts.id
		INNER JOIN devices ON watchers.device_id = devices.id
		LEFT JOIN subreddits ON watchers.type IN(0,2) AND watchers.watchee_id = subreddits.id
		LEFT JOIN users ON watchers.type = 1 AND watchers.watchee_id = users.id
		WHERE
			devices.apns_token = $1 AND
			accounts.reddit_account_id = $2`

	return p.fetch(ctx, query, apns, rid)
}

func (p *postgresWatcherRepository) Create(ctx context.Context, watcher *domain.Watcher) error {
	if err := watcher.Validate(); err != nil {
		return err
	}

	now := time.Now()

	query := `
		INSERT INTO watchers
			(created_at, last_notified_at, label, device_id, account_id, type, watchee_id, author, subreddit, upvotes, keyword, flair, domain)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id`

	return p.conn.QueryRow(
		ctx,
		query,
		now,
		now,
		watcher.Label,
		watcher.DeviceID,
		watcher.AccountID,
		int64(watcher.Type),
		watcher.WatcheeID,
		watcher.Author,
		watcher.Subreddit,
		watcher.Upvotes,
		watcher.Keyword,
		watcher.Flair,
		watcher.Domain,
	).Scan(&watcher.ID)
}

func (p *postgresWatcherRepository) Update(ctx context.Context, watcher *domain.Watcher) error {
	if err := watcher.Validate(); err != nil {
		return err
	}

	query := `
		UPDATE watchers
		SET watchee_id = $2,
			author = $3,
			subreddit = $4,
			upvotes = $5,
			keyword = $6,
			flair = $7,
			domain = $8,
			label = $9
		WHERE id = $1`

	_, err := p.conn.Exec(
		ctx,
		query,
		watcher.ID,
		watcher.WatcheeID,
		watcher.Author,
		watcher.Subreddit,
		watcher.Upvotes,
		watcher.Keyword,
		watcher.Flair,
		watcher.Domain,
		watcher.Label,
	)

	return err
}

func (p *postgresWatcherRepository) IncrementHits(ctx context.Context, id int64) error {
	query := `UPDATE watchers SET hits = hits + 1, last_notified_at = $2 WHERE id = $1`
	_, err := p.conn.Exec(ctx, query, id, time.Now())
	return err
}

func (p *postgresWatcherRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM watchers WHERE id = $1`
	_, err := p.conn.Exec(ctx, query, id)
	return err
}

func (p *postgresWatcherRepository) DeleteByTypeAndWatcheeID(ctx context.Context, typ domain.WatcherType, id int64) error {
	query := `DELETE FROM watchers WHERE type = $1 AND watchee_id = $2`
	_, err := p.conn.Exec(ctx, query, int64(typ), id)
	return err
}
