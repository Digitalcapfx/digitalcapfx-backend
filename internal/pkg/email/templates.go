package email

import (
	"bytes"
	"fmt"
	"html/template"
	"time"
)

// ─── shared layout ────────────────────────────────────────────────────────────

const layout = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>{{.Subject}}</title>
<style>
  body{margin:0;padding:0;background:#f4f6f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,sans-serif;color:#1a1a2e}
  .wrap{max-width:600px;margin:40px auto;background:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 4px 24px rgba(0,0,0,.08)}
  .header{background:linear-gradient(135deg,#0f3460 0%,#16213e 100%);padding:32px 40px;text-align:center}
  .header img{height:40px;margin-bottom:8px}
  .header h1{color:#e94560;margin:0;font-size:22px;letter-spacing:1px;font-weight:700}
  .header p{color:#a8b4c8;margin:4px 0 0;font-size:13px}
  .body{padding:40px}
  .body h2{color:#0f3460;font-size:20px;margin:0 0 16px}
  .body p{color:#444;font-size:15px;line-height:1.7;margin:0 0 16px}
  .otp-box{background:#f0f4ff;border:2px dashed #0f3460;border-radius:8px;text-align:center;padding:24px;margin:24px 0}
  .otp-box .code{font-size:40px;font-weight:800;letter-spacing:10px;color:#0f3460}
  .otp-box p{margin:8px 0 0;font-size:13px;color:#777}
  .btn{display:inline-block;background:linear-gradient(135deg,#e94560,#c73652);color:#fff;text-decoration:none;padding:14px 32px;border-radius:8px;font-weight:600;font-size:15px;margin:8px 0}
  .device-card{background:#f8f9fc;border-left:4px solid #e94560;border-radius:6px;padding:16px 20px;margin:20px 0}
  .device-card p{margin:4px 0;font-size:14px;color:#555}
  .device-card .label{color:#999;font-size:12px;text-transform:uppercase;letter-spacing:.5px}
  .divider{border:none;border-top:1px solid #eee;margin:24px 0}
  .footer{background:#f8f9fc;padding:24px 40px;text-align:center}
  .footer p{color:#aaa;font-size:12px;margin:4px 0;line-height:1.6}
  .footer a{color:#0f3460;text-decoration:none}
  .alert{background:#fff5f5;border:1px solid #fecaca;border-radius:8px;padding:16px;margin:20px 0}
  .alert p{color:#dc2626;margin:0;font-size:14px}
  @media(max-width:600px){.body,.footer{padding:24px}.header{padding:24px}}
</style>
</head>
<body>
<div class="wrap">
  <div class="header">
    <h1>DigitalFX</h1>
    <p>Your Digital Banking Platform</p>
  </div>
  <div class="body">{{.Content}}</div>
  <div class="footer">
    <p>© {{.Year}} DigitalFX · <a href="mailto:support@digitalfx.finance">support@digitalfx.finance</a></p>
    <p>This email was sent to <strong>{{.ToEmail}}</strong>. If you didn't request this, please <a href="mailto:support@digitalfx.finance">contact us</a>.</p>
  </div>
</div>
</body>
</html>`

type layoutData struct {
	Subject string
	Content template.HTML
	Year    int
	ToEmail string
}

func render(subject, toEmail, contentHTML string) string {
	t := template.Must(template.New("layout").Parse(layout))
	var buf bytes.Buffer
	_ = t.Execute(&buf, layoutData{
		Subject: subject,
		Content: template.HTML(contentHTML),
		Year:    time.Now().Year(),
		ToEmail: toEmail,
	})
	return buf.String()
}

// ─── Welcome ──────────────────────────────────────────────────────────────────

func Welcome(toEmail, firstName string) (subject, html string) {
	subject = "Welcome to DigitalFX – Your Account is Ready"
	content := fmt.Sprintf(`
<h2>Welcome, %s! 🎉</h2>
<p>Your DigitalFX account has been created successfully. You now have access to:</p>
<ul style="color:#444;font-size:15px;line-height:2">
  <li><strong>Multi-currency accounts</strong> – XAF, XOF, USD, GBP, EUR</li>
  <li><strong>Instant USD Account</strong> – USDC &amp; USDT stablecoins</li>
  <li><strong>Crypto P2P transfers</strong> – send to any phone number</li>
  <li><strong>Mobile Money on/off ramp</strong> – MTN, Orange, Wave</li>
</ul>
<p>Verify your email address to unlock all features:</p>
<hr class="divider"/>
<p style="font-size:13px;color:#777">Have questions? Our support team is available at <a href="mailto:support@digitalfx.finance" style="color:#0f3460">support@digitalfx.finance</a>.</p>
`, firstName)
	html = render(subject, toEmail, content)
	return
}

// ─── Email Verification OTP ───────────────────────────────────────────────────

func EmailVerificationOTP(toEmail, firstName, code string) (subject, html string) {
	subject = "Verify your DigitalFX email address"
	content := fmt.Sprintf(`
<h2>Verify your email</h2>
<p>Hi %s, please use the code below to verify your email address.</p>
<div class="otp-box">
  <div class="code">%s</div>
  <p>Expires in <strong>10 minutes</strong> &middot; Do not share this code</p>
</div>
<p>If you didn't create a DigitalFX account, you can safely ignore this email.</p>
`, firstName, code)
	html = render(subject, toEmail, content)
	return
}

// ─── Login Notification ───────────────────────────────────────────────────────

type LoginNotificationData struct {
	FirstName  string
	DeviceName string
	DeviceIP   string
	DeviceUA   string
	Time       string
}

func LoginNotification(toEmail string, d LoginNotificationData) (subject, html string) {
	subject = "New sign-in to your DigitalFX account"
	content := fmt.Sprintf(`
<h2>New sign-in detected</h2>
<p>Hi %s, we noticed a new sign-in to your DigitalFX account.</p>
<div class="device-card">
  <p><span class="label">Device</span><br/>%s</p>
  <p><span class="label">IP Address</span><br/>%s</p>
  <p><span class="label">Time</span><br/>%s</p>
</div>
<p>If this was you, no action is needed.</p>
<div class="alert">
  <p>⚠️ <strong>If this wasn't you</strong>, please <a href="mailto:support@digitalfx.finance" style="color:#dc2626">contact support immediately</a> and change your PIN.</p>
</div>
`, d.FirstName, d.DeviceName, d.DeviceIP, d.Time)
	html = render(subject, toEmail, content)
	return
}

// ─── PIN Reset OTP ────────────────────────────────────────────────────────────

func PINResetOTP(toEmail, firstName, code string) (subject, html string) {
	subject = "Reset your DigitalFX PIN"
	content := fmt.Sprintf(`
<h2>PIN Reset Request</h2>
<p>Hi %s, we received a request to reset your DigitalFX PIN. Use the code below:</p>
<div class="otp-box">
  <div class="code">%s</div>
  <p>Expires in <strong>10 minutes</strong> &middot; Do not share this code</p>
</div>
<div class="alert">
  <p>⚠️ If you did not request a PIN reset, please <a href="mailto:support@digitalfx.finance" style="color:#dc2626">contact support immediately</a>.</p>
</div>
`, firstName, code)
	html = render(subject, toEmail, content)
	return
}

// ─── PIN Changed Confirmation ─────────────────────────────────────────────────

func PINChanged(toEmail, firstName, deviceName, changedAt string) (subject, html string) {
	subject = "Your DigitalFX PIN has been changed"
	content := fmt.Sprintf(`
<h2>PIN successfully changed</h2>
<p>Hi %s, your DigitalFX PIN was changed successfully.</p>
<div class="device-card">
  <p><span class="label">Device</span><br/>%s</p>
  <p><span class="label">Time</span><br/>%s</p>
</div>
<div class="alert">
  <p>⚠️ If you didn't make this change, please <a href="mailto:support@digitalfx.finance" style="color:#dc2626">contact support immediately</a>.</p>
</div>
`, firstName, deviceName, changedAt)
	html = render(subject, toEmail, content)
	return
}

// ─── New Device Alert ─────────────────────────────────────────────────────────

func NewDeviceAlert(toEmail, firstName, deviceName, deviceIP, loginTime string) (subject, html string) {
	subject = "New device signed into your DigitalFX account"
	content := fmt.Sprintf(`
<h2>New device connected</h2>
<p>Hi %s, a new device was used to sign into your DigitalFX account.</p>
<div class="device-card">
  <p><span class="label">Device</span><br/>%s</p>
  <p><span class="label">IP Address</span><br/>%s</p>
  <p><span class="label">Time</span><br/>%s</p>
</div>
<p>If this was you, you can manage your connected devices in the app under <strong>Settings → Devices</strong>.</p>
<div class="alert">
  <p>⚠️ <strong>If this wasn't you</strong>, your account may be compromised. Please <a href="mailto:support@digitalfx.finance" style="color:#dc2626">contact support immediately</a>.</p>
</div>
`, firstName, deviceName, deviceIP, loginTime)
	html = render(subject, toEmail, content)
	return
}

// ─── Logout Notification ──────────────────────────────────────────────────────

func LogoutNotification(toEmail, firstName, deviceName, logoutTime string) (subject, html string) {
	subject = "You've been signed out of DigitalFX"
	content := fmt.Sprintf(`
<h2>Signed out</h2>
<p>Hi %s, you (or someone with access to your account) signed out of a device.</p>
<div class="device-card">
  <p><span class="label">Device</span><br/>%s</p>
  <p><span class="label">Time</span><br/>%s</p>
</div>
<p>If this wasn't you, please change your PIN immediately and <a href="mailto:support@digitalfx.finance" style="color:#0f3460">contact support</a>.</p>
`, firstName, deviceName, logoutTime)
	html = render(subject, toEmail, content)
	return
}

// ─── KYC Approved ─────────────────────────────────────────────────────────────

func KYCApproved(toEmail, firstName string) (subject, html string) {
	subject = "🎉 Identity Verified — Your DigitalFX Account is Now Active"
	content := fmt.Sprintf(`
<h2>You're verified, %s!</h2>
<p>Great news — our team has reviewed your identity documents and your account is now <strong>fully activated</strong>.</p>
<p>You can now:</p>
<ul style="color:#444;font-size:15px;line-height:2">
  <li>Send and receive money across borders</li>
  <li>Hold balances in XAF, XOF, USD, GBP and EUR</li>
  <li>Access your Instant USD Account (USDC)</li>
  <li>Trade crypto assets</li>
</ul>
<p>Welcome to the future of digital banking in Africa.</p>
<hr class="divider"/>
<p style="font-size:13px;color:#888">If you have any questions, reach us at <a href="mailto:support@digitalfx.finance" style="color:#0f3460">support@digitalfx.finance</a>.</p>
`, firstName)
	html = render(subject, toEmail, content)
	return
}

// ─── Staff Invite ─────────────────────────────────────────────────────────────

func StaffInvite(toEmail, name, role, roleLabel, inviteURL string) (subject, html string) {
	subject = "You've been invited to join DigitalFX Staff"
	content := fmt.Sprintf(`
<h2>You're invited, %s!</h2>
<p>You've been added to the <strong>DigitalFX</strong> admin platform as a <strong>%s</strong>.</p>
<p>Click the button below to accept your invitation and activate your staff account. The link is valid for <strong>7 days</strong>.</p>
<p style="text-align:center;margin:32px 0">
  <a href="%s" class="btn">Accept Invitation</a>
</p>
<p>Or copy and paste this link into your browser:</p>
<p style="background:#f4f6f9;border-radius:6px;padding:12px 16px;font-size:13px;word-break:break-all;color:#555">%s</p>
<hr class="divider"/>
<p style="font-size:13px;color:#777">If you were not expecting this invitation, you can safely ignore this email. If you have questions, contact <a href="mailto:support@digitalfx.finance" style="color:#0f3460">support@digitalfx.finance</a>.</p>
`, name, roleLabel, inviteURL, inviteURL)
	html = render(subject, toEmail, content)
	return
}

// ─── KYC Rejected ─────────────────────────────────────────────────────────────

func KYCRejected(toEmail, firstName, reason string) (subject, html string) {
	subject = "Action Required — Identity Verification Unsuccessful"
	content := fmt.Sprintf(`
<h2>Hi %s, we couldn't verify your identity</h2>
<p>Unfortunately our team was unable to approve your identity verification. Here's the reason:</p>
<div class="alert">
  <p>%s</p>
</div>
<p>You can resubmit your documents by opening the app and starting a new verification. Please make sure:</p>
<ul style="color:#444;font-size:15px;line-height:2">
  <li>Your document is valid and not expired</li>
  <li>Photos are clear, well-lit, and not cropped</li>
  <li>The selfie matches your ID photo</li>
</ul>
<p>If you believe this is an error, please <a href="mailto:support@digitalfx.finance" style="color:#0f3460">contact our support team</a>.</p>
`, firstName, reason)
	html = render(subject, toEmail, content)
	return
}
