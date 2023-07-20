resource "chainguard_identity" "user" {
  parent_id   = "foo/bar"
  name        = "user"
  description = "example"
  claim_match {
    issuer   = "https://accounts.google.com"
    subject  = "foo"
    audience = "https://console-api.enforce.dev"
  }
}

resource "chainguard_identity" "aws-user" {
  parent_id   = "foo/other"
  name        = "aws-identity"
  description = "literal match to aws-identity"

  aws_identity {
    aws_account = "123456789012"
    aws_user_id = "AROAWSXXXXXXXXX:bob@example.com"
    aws_arn     = "arn:aws:sts::123456789012:assumed-role/example-role/bob@example.com"
  }
}
