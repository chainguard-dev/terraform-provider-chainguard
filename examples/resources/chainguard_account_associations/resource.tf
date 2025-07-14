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
  azure {
    tenant_id = "12345678-1234-1234-1234-123456789012"
    client_ids = {
      "client1" = "11111111-1111-1111-1111-111111111111"
      "client2" = "22222222-2222-2222-2222-222222222222"
    }
  }
}
