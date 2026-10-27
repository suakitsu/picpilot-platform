export function passwordByteLength(password: string) {
  return new TextEncoder().encode(password).length
}

export function validNewPassword(password: string) {
  const bytes = passwordByteLength(password)
  return bytes >= 12 && bytes <= 72
}
