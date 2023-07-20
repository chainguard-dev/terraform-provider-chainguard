resource "chainguard_account_associations" "example" {
  name  = "baz"
  group = "foo/bar"
  amazon {
    account = "1122334455"
  }
  google {
    project_id     = "example"
    project_number = "213411233"
  }
}
