package notification

import (
	"context"

	"github.com/banksampahku/backend/internal/auth"
	"github.com/banksampahku/backend/internal/utils"
)

// Service berisi logika baca & tandai-dibaca notifikasi untuk user yang login
// (admin maupun nasabah). Pembuatan notifikasi ada di notification.go (CreateTx).
type Service struct {
	repo *Repository
}

// NewService membuat service notifikasi
func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// List mengambil notifikasi user + jumlah belum dibaca (untuk badge lonceng)
func (s *Service) List(ctx context.Context, user *auth.User, q ListQuery) (*ListResponse, error) {
	items, total, err := s.repo.List(ctx, user.ID, q)
	if err != nil {
		return nil, err
	}
	unread, err := s.repo.UnreadCount(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &ListResponse{Items: items, Meta: utils.NewPageMeta(q.PageQuery, total), UnreadCount: unread}, nil
}

// UnreadCount menghitung notifikasi user yang belum dibaca
func (s *Service) UnreadCount(ctx context.Context, user *auth.User) (int64, error) {
	return s.repo.UnreadCount(ctx, user.ID)
}

// MarkRead menandai satu notifikasi milik user sebagai dibaca
func (s *Service) MarkRead(ctx context.Context, user *auth.User, id string) (*Item, error) {
	return s.repo.MarkRead(ctx, user.ID, id)
}

// MarkAllRead menandai semua notifikasi user sebagai dibaca
func (s *Service) MarkAllRead(ctx context.Context, user *auth.User) (int64, error) {
	return s.repo.MarkAllRead(ctx, user.ID)
}
