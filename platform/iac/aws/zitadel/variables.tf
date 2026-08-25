variable "aws_region" {
  description = "AWS region for the foundation."
  type        = string
  default     = "us-east-1"
}

variable "offline_validation" {
  description = "Skip AWS credential/account discovery for an offline graph plan; never use true for apply."
  type        = bool
  default     = false
}

variable "name" {
  description = "Lowercase deployment name used in resource names."
  type        = string
  default     = "smt-zitadel"

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$", var.name))
    error_message = "name must be 3-32 characters of lowercase letters, numbers, or hyphens."
  }
}

variable "environment" {
  description = "Environment label used in resource names and tags."
  type        = string
  default     = "prod"

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{1,15}[a-z0-9]$", var.environment))
    error_message = "environment must be 3-17 characters of lowercase letters, numbers, or hyphens."
  }
}

variable "availability_zones" {
  description = "Exactly two AZs for the foundation."
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b"]

  validation {
    condition     = length(var.availability_zones) == 2 && var.availability_zones[0] != var.availability_zones[1]
    error_message = "availability_zones must contain two distinct availability zones."
  }
}

variable "vpc_cidr" {
  description = "CIDR range for the deployment VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "public_subnet_cidrs" {
  description = "Two public subnet CIDRs, one per configured AZ."
  type        = list(string)
  default     = ["10.42.0.0/20", "10.42.16.0/20"]

  validation {
    condition     = length(var.public_subnet_cidrs) == 2
    error_message = "public_subnet_cidrs must contain exactly two CIDRs."
  }
}

variable "private_subnet_cidrs" {
  description = "Two private subnet CIDRs, one per configured AZ."
  type        = list(string)
  default     = ["10.42.32.0/20", "10.42.48.0/20"]

  validation {
    condition     = length(var.private_subnet_cidrs) == 2
    error_message = "private_subnet_cidrs must contain exactly two CIDRs."
  }
}

variable "ami_id" {
  description = "Hardened AMI with Docker Compose v2 and the SSM agent installed."
  type        = string

  validation {
    condition     = can(regex("^ami-[a-z0-9]+$", var.ami_id))
    error_message = "ami_id must be an AWS AMI identifier."
  }
}

variable "instance_type" {
  description = "EC2 instance class for the initial single-host Compose runtime."
  type        = string
  default     = "t3.small"
}

variable "root_volume_size_gib" {
  description = "Encrypted root volume size for the Compose host."
  type        = number
  default     = 80

  validation {
    condition     = var.root_volume_size_gib >= 30
    error_message = "root_volume_size_gib must be at least 30 GiB."
  }
}

variable "key_name" {
  description = "Optional EC2 key pair name. Prefer SSM instead of SSH."
  type        = string
  default     = null
}

variable "certificate_arn" {
  description = "ACM certificate ARN for the public HTTPS ALB listener."
  type        = string

  validation {
    condition     = can(regex("^arn:[^:]+:acm:[^:]+:[0-9]{12}:certificate/[a-f0-9-]+$", var.certificate_arn))
    error_message = "certificate_arn must be an ACM certificate ARN."
  }
}

variable "domain_name" {
  description = "Public ZITADEL domain used by the generated Compose environment."
  type        = string
  default     = ""
}

variable "route53_zone_id" {
  description = "Optional Route53 hosted zone ID for an ALB alias record."
  type        = string
  default     = null
}

variable "route53_record_name" {
  description = "Optional Route53 record name. Both this and route53_zone_id enable the alias."
  type        = string
  default     = ""
}

variable "ssh_ingress_cidrs" {
  description = "Optional SSH source CIDRs; leave empty to keep SSH closed."
  type        = list(string)
  default     = []
}

variable "compose_target_port" {
  description = "HTTP/2 target port exposed by the Compose reverse proxy."
  type        = number
  default     = 8081
}

variable "rds_engine_version" {
  description = "PostgreSQL major/minor version supported by the pinned ZITADEL release."
  type        = string
  default     = "16.4"
}

variable "rds_instance_class" {
  description = "Private Multi-AZ RDS PostgreSQL instance class."
  type        = string
  default     = "db.t4g.medium"
}

variable "rds_storage_gib" {
  description = "Initial encrypted RDS storage size."
  type        = number
  default     = 100

  validation {
    condition     = var.rds_storage_gib >= 20
    error_message = "rds_storage_gib must be at least 20 GiB."
  }
}

variable "rds_backup_retention_days" {
  description = "RDS automated backup retention period."
  type        = number
  default     = 14

  validation {
    condition     = var.rds_backup_retention_days >= 7 && var.rds_backup_retention_days <= 35
    error_message = "rds_backup_retention_days must be between 7 and 35 days."
  }
}

variable "rds_deletion_protection" {
  description = "Protect the RDS instance from accidental deletion."
  type        = bool
  default     = true
}

variable "rds_skip_final_snapshot" {
  description = "Skip the final RDS snapshot only for a deliberate teardown."
  type        = bool
  default     = false
}

variable "cloudwatch_retention_days" {
  description = "Retention for the Compose host log group."
  type        = number
  default     = 30
}

variable "zitadel_image" {
  description = "Pinned ZITADEL API image reference; use a digest for production."
  type        = string
  default     = "ghcr.io/zitadel/zitadel:v4.16.2"
}

variable "zitadel_login_image" {
  description = "Pinned ZITADEL Login V2 image reference; use a digest for production."
  type        = string
  default     = "ghcr.io/zitadel/zitadel-login:v4.16.2"
}

variable "proxy_image" {
  description = "Pinned reverse-proxy image reference; use a digest for production."
  type        = string
  default     = "traefik:v3.5.3"
}
