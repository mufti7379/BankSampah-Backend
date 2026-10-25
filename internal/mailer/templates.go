package mailer

import (
	"bytes"
	"fmt"
	"html/template"
)

// PasswordResetData adalah data untuk template email reset password
type PasswordResetData struct {
	FullName      string
	ResetURL      string
	ExpireMinutes int
	Locked        bool // true jika akun sedang terkunci saat user meminta reset
}

// passwordResetHTML memakai warna design system PRD Bab 4 (dark + emerald).
// Style ditulis inline karena banyak klien email mengabaikan tag <style>.
var passwordResetHTML = template.Must(template.New("reset").Parse(`<!DOCTYPE html>
<html lang="id">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#0f131a;font-family:Inter,Segoe UI,Arial,sans-serif;color:#f2f5f8;">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#0f131a;padding:32px 16px;">
    <tr><td align="center">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;background:#181d27;border:1px solid #3d4556;border-radius:16px;overflow:hidden;">
        <tr><td style="height:4px;background:linear-gradient(90deg,#14e09a,#2ad0f2);"></td></tr>
        <tr><td style="padding:32px 32px 8px;">
          <p style="margin:0;font-size:20px;font-weight:800;color:#14e09a;">BankSampahKu</p>
        </td></tr>
        <tr><td style="padding:8px 32px 32px;font-size:15px;line-height:1.6;">
          <p style="margin:0 0 16px;">Halo <strong>{{.FullName}}</strong>,</p>
          {{if .Locked}}
          <p style="margin:0 0 16px;">Kami menerima permintaan untuk mengatur ulang password akun BankSampahKu Anda. Saat ini akun Anda <strong style="color:#f5b43d;">terkunci sementara</strong> karena terdeteksi 5 kali percobaan login dengan password yang salah.</p>
          <p style="margin:0 0 24px;">Silakan buat password baru melalui tombol di bawah. Setelah password diganti, akun Anda langsung bisa digunakan kembali.</p>
          {{else}}
          <p style="margin:0 0 24px;">Kami menerima permintaan untuk mengatur ulang password akun BankSampahKu Anda. Klik tombol di bawah untuk membuat password baru.</p>
          {{end}}
          <table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:12px;background:#14e09a;">
            <a href="{{.ResetURL}}" style="display:inline-block;padding:14px 28px;font-weight:700;color:#0f131a;text-decoration:none;">Atur Ulang Password</a>
          </td></tr></table>
          <p style="margin:24px 0 8px;color:#99a3b3;font-size:13px;">Link ini berlaku selama <strong>{{.ExpireMinutes}} menit</strong> dan hanya dapat digunakan satu kali. Jika tombol tidak berfungsi, salin link berikut ke browser Anda:</p>
          <p style="margin:0 0 24px;word-break:break-all;font-family:JetBrains Mono,Consolas,monospace;font-size:12px;color:#2ad0f2;">{{.ResetURL}}</p>
          <p style="margin:0;color:#99a3b3;font-size:13px;">Jika Anda tidak merasa melakukan ini, abaikan email ini. Password Anda tidak akan berubah. {{if .Locked}}Demi keamanan, sebaiknya segera hubungi admin bank sampah sekolah.{{end}}</p>
        </td></tr>
        <tr><td style="padding:16px 32px;border-top:1px solid #3d4556;color:#99a3b3;font-size:12px;">
          Email ini dikirim otomatis oleh sistem BankSampahKu. Mohon tidak membalas email ini.
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

// PasswordResetEmail menyusun email reset password (teks & HTML)
func PasswordResetEmail(to string, data PasswordResetData) (Message, error) {
	var html bytes.Buffer
	if err := passwordResetHTML.Execute(&html, data); err != nil {
		return Message{}, fmt.Errorf("gagal menyusun template email: %w", err)
	}

	subject := "Atur Ulang Password Akun BankSampahKu"
	intro := "Kami menerima permintaan untuk mengatur ulang password akun BankSampahKu Anda."
	if data.Locked {
		intro = "Kami menerima permintaan untuk mengatur ulang password akun BankSampahKu Anda.\n" +
			"Saat ini akun Anda terkunci sementara karena 5 kali percobaan login dengan password yang salah.\n" +
			"Buat password baru melalui link berikut; setelah itu akun langsung bisa digunakan kembali."
	}

	text := fmt.Sprintf(
		"Halo %s,\n\n%s\n\n%s\n\nLink berlaku %d menit dan hanya dapat digunakan satu kali.\n"+
			"Jika Anda tidak merasa melakukan ini, abaikan email ini.\n\nSalam,\nBankSampahKu",
		data.FullName, intro, data.ResetURL, data.ExpireMinutes,
	)

	return Message{
		To:       to,
		ToName:   data.FullName,
		Subject:  subject,
		TextBody: text,
		HTMLBody: html.String(),
	}, nil
}
