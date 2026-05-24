resource "aws_instance" "foo" {
  count = 1
  ami   = "ami-123"
}

# Mixed: a bare reference and an already-indexed one. addindex should
# abort because the user must resolve the existing index first.
output "indexed" {
  value = aws_instance.foo[0].id
}

output "bare" {
  value = aws_instance.foo.public_ip
}
