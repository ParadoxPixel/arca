package store

import (
	"arca/internal/store/models"
)

func (s *Store) CreateToken(token *models.APIToken) error {
	if token.ID == "" {
		token.ID = NewID()
	}
	if token.CreatedAt == 0 {
		token.CreatedAt = NowMillis()
	}
	return s.db.Create(token).Error
}

func (s *Store) ListTokens(userID string) ([]models.APIToken, error) {
	var tokens []models.APIToken
	err := s.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error
	return tokens, err
}

func (s *Store) DeleteToken(id, userID string) (int64, error) {
	result := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.APIToken{})
	return result.RowsAffected, result.Error
}

func (s *Store) UserByTokenHash(hash string) (*models.User, string, error) {
	var token models.APIToken
	now := NowMillis()
	err := s.db.
		Where("token_hash = ? AND (expires_at IS NULL OR expires_at > ?)", hash, now).
		First(&token).Error
	if err != nil {
		return nil, "", err
	}

	user, err := s.UserByID(token.UserID)
	if err != nil {
		return nil, "", err
	}
	return user, token.ID, nil
}

func (s *Store) TouchToken(id string) error {
	now := NowMillis()
	return s.db.Model(&models.APIToken{}).Where("id = ?", id).Update("last_used_at", now).Error
}
