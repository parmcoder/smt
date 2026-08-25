---
type: infrastructure-foundation
status: active
owner: platform
tags:
  - smt
  - zitadel
  - aws
  - opentofu
---
# SMT ZITADEL AWS foundation

This OpenTofu module provisions the first AWS foundation for the self-hosted
ZITADEL Compose runtime. It intentionally uses one EC2/Compose host behind an
HTTPS ALB and a private Multi-AZ RDS PostgreSQL instance. It is a deployment
foundation, not a high-availability ZITADEL cluster. ZITADEL's later three-node
production topology and zero-downtime upgrade acceptance remain a follow-up.

The module creates:

- a two-AZ VPC with public ALB subnets and private runtime/RDS subnets;
- a single NAT gateway for private-host egress;
- an HTTPS ALB with an HTTP redirect and an HTTP/2 target group on the generated
  ZITADEL proxy's default port (8081) and `/debug/ready`;
- a one-instance Auto Scaling Group with SSM access and encrypted storage;
- private Multi-AZ RDS PostgreSQL with automated backups and an AWS-managed
  master secret;
- KMS encryption, an external-runtime secret metadata entry, CloudWatch logs,
  CPU/target-health alarms, and an optional Route53 alias.

The generated SMT Compose contract remains the source of the service
configuration. Upload that contract and the production secret references to
the host through SSM. Do not put `ZITADEL_MASTERKEY`, database passwords, OIDC
client secrets, or populated `.env` files in Git or OpenTofu variables. The
`zitadel_runtime_secret_arn` output is metadata only; this module deliberately
does not create a secret value or a client secret.

`domain_name` is the external issuer/domain input. When `route53_zone_id` is
also set, it creates an ALB alias for `route53_record_name` when supplied, or
for `domain_name` otherwise. Certificate issuance and DNS ownership remain
operator inputs.

## Validate offline

Use a local OpenTofu binary and a provider cache/lock file supplied by the
deployment environment:

```sh
tofu fmt -check
tofu init -backend=false
tofu validate
tofu plan -refresh=false -lock=false -var-file=terraform.tfvars
```

The plan requires a real AMI ID and ACM certificate ARN. `terraform.tfvars.example`
contains shape-only examples; copy it to an ignored `terraform.tfvars` and
replace those values before planning. For a provider-pinned graph check without
AWS credentials, add `-var offline_validation=true`; this skips credential and
account discovery and must never be used for apply. AWS credentials, DNS
ownership, and a certificate are required for apply and acceptance.

## Runtime handoff

1. Apply the foundation with `deletion_protection = true` and the production
   image references pinned to immutable digests.
2. Store the RDS master secret ARN and a separately generated ZITADEL runtime
   DSN/master-key payload in Secrets Manager. Provision the dedicated `zitadel`
   database and user through the Compose bootstrap contract on the shared RDS
   server; do not reuse the application database credentials.
3. Through SSM, install/upload Docker Compose v2 and the generated root
   `compose.yaml` plus a mode-appropriate `.env`. Start the runtime and verify
   `/debug/ready`, `/debug/healthz`, discovery, JWKS, metrics, browser OIDC,
   and gRPC/HTTP2 traffic through the ALB.
4. Capture image digests, tool versions, target health, RDS connectivity, secret
   retrieval/rotation, backup/restore, and EC2 replacement evidence.

The initial topology has one NAT gateway and one desired EC2 instance. Scale
out, multi-node ZITADEL, zero-downtime upgrades, and failure-domain drills are
explicitly unverified until the later HA task passes.

## Verification snapshot

The contract was checked on 2026-08-23 with OpenTofu `v1.12.3` and the pinned
`hashicorp/aws` provider `6.0.0`:

```text
tofu fmt -check PASS
tofu validate PASS
tofu plan ... PASS (35 resources to add; offline_validation=true)
go test . PASS
```

The plan used placeholder AMI/certificate inputs and dummy credentials only to
exercise the provider-pinned graph. No AWS resource was applied; live account,
DNS, certificate, RDS, ALB HTTP/2, secret-rotation, and replacement checks
remain for the verification child.
