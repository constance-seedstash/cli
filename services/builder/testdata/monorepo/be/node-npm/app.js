const express = require('express')
const { HELLO_WORLD } = require('../../common/lib/greet.js');
const app = express()
const port = 3000

app.get('/hi', (req, res) => {
    res.send({ "greet": HELLO_WORLD })
})

app.listen(port, () => {
    console.log(`Example app listening on port ${port}`)
})
