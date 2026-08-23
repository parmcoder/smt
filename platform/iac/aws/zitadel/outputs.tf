output "vpc_id" {
  description = "Deployment VPC ID."
  value       = aws_vpc.this.id
}

output "private_subnet_ids" {
  description = "Private subnet IDs used by the EC2 ASG and RDS."
  value       = aws_subnet.private[*].id
}

output "alb_dns_name" {
  description = "ALB DNS name for the configured HTTPS listener."
  value       = aws_lb.this.dns_name
}

output "alb_target_group_arn" {
  description = "HTTP/2 target group ARN."
  value       = aws_lb_target_group.runtime.arn
}

output "rds_endpoint" {
  description = "Private RDS endpoint; credentials are not output."
  value       = aws_db_instance.runtime.address
}

output "rds_master_secret_arn" {
  description = "AWS-managed RDS master secret ARN; the secret value is never output."
  value       = aws_db_instance.runtime.master_user_secret[0].secret_arn
}

output "zitadel_runtime_secret_arn" {
  description = "Secret metadata ARN to be populated and rotated by the deployment handoff."
  value       = aws_secretsmanager_secret.zitadel_runtime.arn
}

output "runtime_asg_name" {
  description = "Single-host Compose Auto Scaling Group name."
  value       = aws_autoscaling_group.runtime.name
}

output "cloudwatch_log_group_name" {
  description = "CloudWatch log group for the ZITADEL runtime."
  value       = aws_cloudwatch_log_group.runtime.name
}

output "route53_record_fqdn" {
  description = "Optional Route53 alias FQDN."
  value       = try(aws_route53_record.runtime[0].fqdn, null)
}
