package models

// UserRole defines the role of a user in the system
type UserRole string

const (
	RoleUser       UserRole = "user"
	RoleBooking    UserRole = "booking"    // petugas booking desk / kiosk
	RoleAdmin      UserRole = "admin"      // approve/reject bookings, kelola rooms
	RoleSuperAdmin UserRole = "superadmin" // kelola semua user + semua fitur admin
)

// ValidRoles daftar role yang valid untuk validasi input
var ValidRoles = map[UserRole]bool{
	RoleUser: true, RoleBooking: true,
	RoleAdmin: true, RoleSuperAdmin: true,
}

// User represents a registered user
type User struct {
	ID           string   `json:"id" db:"id"`
	Name         string   `json:"name" db:"name"`
	Email        string   `json:"email" db:"email"`
	Password     string   `json:"-" db:"password"` // never exposed in JSON
	ProfileImage *string  `json:"profileImage" db:"profile_image"`
	City         *string  `json:"city" db:"city"`
	Role         UserRole `json:"role" db:"role"`
	CreatedAt    int64    `json:"createdAt" db:"created_at"`
	UpdatedAt    *int64   `json:"updatedAt" db:"updated_at"`
}

func (u *User) IsSuperAdmin() bool { return u.Role == RoleSuperAdmin }
func (u *User) IsAdmin() bool      { return u.Role == RoleAdmin || u.Role == RoleSuperAdmin }
func (u *User) IsBooking() bool    { return u.Role == RoleBooking }

// UserResponse is the public view of User (no password)
type UserResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	ProfileImage *string  `json:"profileImage"`
	City         *string  `json:"city"`
	Role         UserRole `json:"role"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    *int64   `json:"updatedAt"`
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:           u.ID,
		Name:         u.Name,
		Email:        u.Email,
		ProfileImage: u.ProfileImage,
		City:         u.City,
		Role:         u.Role,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}

type RegisterRequest struct {
	Name     string `json:"name" binding:"required,min=2,max=100"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type UpdateUserRequest struct {
	Name         *string `json:"name"`
	ProfileImage *string `json:"profileImage"`
	City         *string `json:"city"`
}

type UpdateCityRequest struct {
	City string `json:"city" binding:"required"`
}

// ChangePasswordRequest untuk PATCH /api/auth/me/password
// Firebase Auth punya built-in ini, di Go backend kita implement manual
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required,min=6"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

// ChangeRoleRequest untuk PATCH /api/admin/users/:id/role (superadmin only)
type ChangeRoleRequest struct {
	Role UserRole `json:"role" binding:"required"`
}

// CreateManagedUserRequest untuk POST /api/admin/users (superadmin only)
type CreateManagedUserRequest struct {
	Name         string   `json:"name" binding:"required,min=2,max=100"`
	Email        string   `json:"email" binding:"required,email"`
	Password     string   `json:"password" binding:"required,min=6"`
	Role         UserRole `json:"role" binding:"required"`
	ProfileImage *string  `json:"profileImage"`
	City         *string  `json:"city"`
}

type UserListFilter struct {
	Role   UserRole `form:"role"`
	Search string   `form:"search"`
}
