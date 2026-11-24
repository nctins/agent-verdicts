package authentication

// Tests hash many passwords; full-strength PBKDF2 would make the suite
// slow without testing anything extra (the iteration count is stored in
// each hash and honoured on verify).
func init() { passwordIterations = 1000 }

var CheckPassword = checkPassword
