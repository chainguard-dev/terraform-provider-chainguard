resource "chainguard_cluster" "foo" {
  parent_id = "foo/bar"

  name        = "example cluster"
  description = "part of foo/bar, hosted in gke"

  managed {
    provider = "gke"
    info {
      server                     = "https://api-server.example.com"
      certificate_authority_data = <<-EOF
        -----BEGIN CERTIFICATE-----
        MIIELDCCApSgAwIBAgIQZ3VU1FZ0gZ/VlWYDYv6xzzANBgkqhkiG9w0BAQsFADAv
        ..........
        -----END CERTIFICATE--------
      EOF
    }
  }
}
