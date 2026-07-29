package store

import (
	"arca/internal/store/models"
)

func (s *Store) CreateSession(session *models.Session) error {
	return s.db.Create(session).Error
}

func (s *Store) UserBySession(hash string) (*models.User, error) {
	var user models.User
	err := s.db.
		Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.id = ? AND sessions.expires_at > ?", hash, NowMillis()).
		First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) DeleteSession(hash string) error {
	return s.db.Where("id = ?", hash).Delete(&models.Session{}).Error
}

func (s *Store) DeleteUserSessions(userID string) error {
	return s.db.Where("user_id = ?", userID).Delete(&models.Session{}).Error
}

func (s *Store) DeleteExpiredSessions() (int64, error) {
	result := s.db.Where("expires_at <= ?", NowMillis()).Delete(&models.Session{})
	return result.RowsAffected, result.Error
}
