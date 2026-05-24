resource "aws_instance" "foo" {
  ami = "ami-123"
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo.id
}
