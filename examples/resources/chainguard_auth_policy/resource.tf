# Sessions last up to 30 days from login. At 30 days or less, use does not
# extend a session.
resource "chainguard_auth_policy" "example" {
  parent_id        = "fb694596eb1678321f94eec283e1e0be690f655c"
  session_duration = "720h"
}
