package com.example.service;

import java.io.StringReader;
import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import org.w3c.dom.Document;
import org.xml.sax.InputSource;

public class XmlProcessor {
    public Document parseInvoice(String xmlContent) throws Exception {
        DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
        
        // BUG: XXE Vulnerability
        // Factory is not configured to disable external entity processing.
        // An attacker can send XML with <!ENTITY xxe SYSTEM "file:///etc/passwd">
        
        DocumentBuilder builder = factory.newDocumentBuilder();
        InputSource is = new InputSource(new StringReader(xmlContent));
        return builder.parse(is);
    }

    public String extractValue(Document doc, String tagName) {
        return doc.getElementsByTagName(tagName).item(0).getTextContent();
    }
}
