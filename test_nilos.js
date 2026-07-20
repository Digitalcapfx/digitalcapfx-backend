const crypto = require('crypto');
function computeExtraHeaders(apiKeyId, apiKeySecret, urlPath, rawBody) {
    const signature = crypto.createHmac('sha256', apiKeySecret)
        .update(${urlPath})
        .digest('hex');
    return { 'X-Api-Key': apiKeyId, 'X-Api-Signature': signature };
}
console.log(computeExtraHeaders('my-key-id', 'my-secret-key', '/v1/accounts', '{"amount":100}'));
