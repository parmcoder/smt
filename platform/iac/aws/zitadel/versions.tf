terraform {
  required_version = ">= 1.8.0, < 2.0.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.0.0"
    }
  }
}

provider "aws" {
  region                      = var.aws_region
  skip_credentials_validation = var.offline_validation
  skip_metadata_api_check     = var.offline_validation
  skip_requesting_account_id  = var.offline_validation

  default_tags {
    tags = local.common_tags
  }
}
