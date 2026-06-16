// Package infrastructure 提供 post 领域基础设施层实现。
package infrastructure

import (
	"context"
	"errors"

	"interestBar/pkg/domains/post/domain"
	sharedomain "interestBar/pkg/shared/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// postRepoPG 基于 GORM 的 PostRepository 实现。
type postRepoPG struct {
	db *gorm.DB
}

// NewPostRepository 构造 PostRepository。
func NewPostRepository(db *gorm.DB) domain.PostRepository {
	return &postRepoPG{db: db}
}

func (r *postRepoPG) GetByID(ctx context.Context, postID uuid.UUID) (*domain.Post, error) {
	var p domain.Post
	err := r.db.WithContext(ctx).Where("id = ? AND deleted = ?", postID, 0).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrPostNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *postRepoPG) Create(ctx context.Context, post *domain.Post) error {
	if post.ID == uuid.Nil {
		post.ID = sharedomain.NewID()
	}
	return r.db.WithContext(ctx).Create(post).Error
}

func (r *postRepoPG) GetMediaByPostIDs(ctx context.Context, postIDs []uuid.UUID) (map[uuid.UUID]domain.MediaExtraJSON, error) {
	if len(postIDs) == 0 {
		return make(map[uuid.UUID]domain.MediaExtraJSON), nil
	}
	var posts []domain.Post
	err := r.db.WithContext(ctx).Select("id, media_extra").Where("id IN ?", postIDs).Find(&posts).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]domain.MediaExtraJSON, len(posts))
	for _, p := range posts {
		result[p.ID] = p.MediaExtra
	}
	return result, nil
}

func (r *postRepoPG) IsLiked(ctx context.Context, userID, postID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.PostLike{}).
		Where("user_id = ? AND post_id = ? AND deleted = ?", userID, postID, domain.PostLikeActive).
		Count(&count).Error
	return count > 0, err
}
