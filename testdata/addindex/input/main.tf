resource "aws_instance" "foo" {
  ami           = "ami-123"
  instance_type = var.size
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo.id
}

output "ip" {
  value = aws_instance.foo.public_ip
}

# Index / splat applied to a deeper attribute; the resource ref itself
# is still bare, so addindex must rewrite the inner traversal rather than
# abort.
output "tag" {
  value = aws_instance.foo.tags[var.key]
}

output "sg_ids" {
  value = aws_instance.foo.security_group_ids[*]
}

# Non-matching: different name, different type.
output "other_name" {
  value = aws_instance.bar.id
}

output "other_type" {
  value = aws_eip.addr.id
}

# Non-matching IndexExpr: a different root with a len-2 collection.
# The pre-check must not abort on this; it's unrelated to the target.
output "config_value" {
  value = var.config[var.key]
}
